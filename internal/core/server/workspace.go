package server

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/deref/exo/internal/chrono"
	"github.com/deref/exo/internal/core/api"
	state "github.com/deref/exo/internal/core/state/api"
	"github.com/deref/exo/internal/gensym"
	logd "github.com/deref/exo/internal/logd/api"
	"github.com/deref/exo/internal/manifest"
	"github.com/deref/exo/internal/providers/core"
	"github.com/deref/exo/internal/providers/core/components/invalid"
	"github.com/deref/exo/internal/providers/core/components/log"
	"github.com/deref/exo/internal/providers/docker"
	"github.com/deref/exo/internal/providers/docker/components/container"
	"github.com/deref/exo/internal/providers/docker/components/network"
	"github.com/deref/exo/internal/providers/docker/components/volume"
	"github.com/deref/exo/internal/providers/unix/components/process"
	"github.com/deref/exo/internal/task"
	"github.com/deref/exo/internal/util/errutil"
	"github.com/deref/exo/internal/util/jsonutil"
	"github.com/deref/exo/internal/util/logging"
	dockerclient "github.com/docker/docker/client"
	psprocess "github.com/shirou/gopsutil/v3/process"
)

type Workspace struct {
	ID          string
	VarDir      string
	Store       state.Store
	SyslogPort  uint
	Logger      logging.Logger
	Docker      *dockerclient.Client
	TaskTracker *task.TaskTracker
}

func (ws *Workspace) Describe(ctx context.Context, input *api.DescribeInput) (*api.DescribeOutput, error) {
	description, err := ws.describe(ctx)
	if err != nil {
		return nil, err
	}
	return &api.DescribeOutput{
		Description: *description,
	}, nil
}

func (ws *Workspace) describe(ctx context.Context) (*api.WorkspaceDescription, error) {
	output, err := ws.Store.DescribeWorkspaces(ctx, &state.DescribeWorkspacesInput{
		IDs: []string{ws.ID},
	})
	if err != nil {
		return nil, err
	}
	if len(output.Workspaces) != 1 {
		return nil, fmt.Errorf("invalid workspace: %q", ws.ID)
	}
	return &api.WorkspaceDescription{
		ID:   ws.ID,
		Root: output.Workspaces[0].Root,
	}, nil
}

func (ws *Workspace) Destroy(ctx context.Context, input *api.DestroyInput) (*api.DestroyOutput, error) {
	describeOutput, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{})
	if err != nil {
		return nil, fmt.Errorf("describing components: %w", err)
	}
	// TODO: Parallelism / bulk delete.
	for _, component := range describeOutput.Components {
		if err := ws.deleteComponent(ctx, component.ID); err != nil {
			return nil, fmt.Errorf("deleting %s: %w", component.Name, err)
		}
	}
	if _, err := ws.Store.RemoveWorkspace(ctx, &state.RemoveWorkspaceInput{
		ID: ws.ID,
	}); err != nil {
		return nil, fmt.Errorf("removing workspace from store: %w", err)
	}
	return &api.DestroyOutput{}, nil
}

func (ws *Workspace) Apply(ctx context.Context, input *api.ApplyInput) (*api.ApplyOutput, error) {
	description, err := ws.describe(ctx)
	if err != nil {
		return nil, fmt.Errorf("describing workspace: %w", err)
	}
	res := ws.loadManifest(description.Root, input)
	if res.Err != nil {
		return nil, err
	}
	m := res.Manifest

	describeOutput, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{})
	if err != nil {
		return nil, fmt.Errorf("describing components: %w", err)
	}

	// Index old components by name.
	oldComponents := make(map[string]api.ComponentDescription, len(describeOutput.Components))
	for _, component := range describeOutput.Components {
		oldComponents[component.Name] = component
	}

	// TODO: Handle partial failures.

	// Apply component upserts.
	newComponents := make(map[string]manifest.Component, len(m.Components))
	for _, newComponent := range m.Components {
		name := newComponent.Name
		newComponents[name] = newComponent
		if oldComponent, exists := oldComponents[name]; exists {
			// Update existing component.
			if err := ws.updateComponent(ctx, oldComponent, newComponent); err != nil {
				return nil, fmt.Errorf("updating %q: %w", name, err)
			}
		} else {
			// Create new component.
			if _, err := ws.createComponent(ctx, newComponent); err != nil {
				return nil, fmt.Errorf("adding %q: %w", name, err)
			}
		}
	}

	// Apply component deletions.
	// TODO: Dispose in parallel. Optionally await deletion.
	for name, oldComponent := range oldComponents {
		if _, keep := newComponents[name]; keep {
			continue
		}
		if err := ws.deleteComponent(ctx, oldComponent.ID); err != nil {
			return nil, fmt.Errorf("deleting %q: %w", name, err)
		}
	}

	return &api.ApplyOutput{
		Warnings: res.Warnings,
	}, nil
}

func (ws *Workspace) Resolve(ctx context.Context, input *api.ResolveInput) (*api.ResolveOutput, error) {
	storeOutput, err := ws.Store.Resolve(ctx, &state.ResolveInput{
		WorkspaceID: ws.ID,
		Refs:        input.Refs,
	})
	if err != nil {
		return nil, err
	}
	var output api.ResolveOutput
	output.IDs = make([]*string, len(storeOutput.IDs))
	for i, id := range storeOutput.IDs {
		output.IDs[i] = id
	}
	return &output, err
}

func (ws *Workspace) DescribeComponents(ctx context.Context, input *api.DescribeComponentsInput) (*api.DescribeComponentsOutput, error) {
	stateOutput, err := ws.Store.DescribeComponents(ctx, &state.DescribeComponentsInput{
		WorkspaceID: ws.ID,
		IDs:         input.IDs,
		Types:       input.Types,
	})
	if err != nil {
		return nil, err
	}
	output := &api.DescribeComponentsOutput{
		Components: []api.ComponentDescription{},
	}
	for _, component := range stateOutput.Components {
		output.Components = append(output.Components, api.ComponentDescription{
			ID:          component.ID,
			Name:        component.Name,
			Type:        component.Type,
			Spec:        component.Spec,
			State:       component.State,
			Created:     component.Created,
			Initialized: component.Initialized,
			Disposed:    component.Disposed,
		})
	}
	return output, nil
}

func (ws *Workspace) newController(ctx context.Context, typ string) Controller {
	description, err := ws.describe(ctx)
	if err != nil {
		return &invalid.Invalid{
			Err: fmt.Errorf("workspace error: %w", err),
		}
	}
	component := core.Component{
		ComponentID:   description.ID,
		WorkspaceRoot: description.Root,
		Logger:        ws.Logger,
	}
	switch typ {
	case "process":
		return &process.Process{
			Component:  component,
			SyslogPort: ws.SyslogPort,
		}

	case "container":
		return &container.Container{
			Component: docker.Component{
				Component: component,
				Docker:    ws.Docker,
			},
			SyslogPort: ws.SyslogPort,
		}

	case "network":
		return &network.Network{
			Component: docker.Component{
				Component: component,
				Docker:    ws.Docker,
			},
		}

	case "volume":
		return &volume.Volume{
			Component: docker.Component{
				Component: component,
				Docker:    ws.Docker,
			},
		}

	default:
		return &invalid.Invalid{
			Err: fmt.Errorf("unsupported component type: %q", typ),
		}
	}
}

func (ws *Workspace) CreateComponent(ctx context.Context, input *api.CreateComponentInput) (*api.CreateComponentOutput, error) {
	id, err := ws.createComponent(ctx, manifest.Component{
		Name: input.Name,
		Type: input.Type,
		Spec: input.Spec,
	})
	if err != nil {
		return nil, err
	}
	return &api.CreateComponentOutput{
		ID: id,
	}, nil
}

func (ws *Workspace) createComponent(ctx context.Context, component manifest.Component) (id string, err error) {
	if err := manifest.ValidateName(component.Name); err != nil {
		return "", errutil.HTTPErrorf(http.StatusBadRequest, "component name %q invalid: %w", component.Name, err)
	}

	id = gensym.RandomBase32()

	if _, err := ws.Store.AddComponent(ctx, &state.AddComponentInput{
		WorkspaceID: ws.ID,
		ID:          id,
		Name:        component.Name,
		Type:        component.Type,
		Spec:        component.Spec,
		Created:     chrono.NowString(ctx),
	}); err != nil {
		return "", fmt.Errorf("adding component: %w", err)
	}

	if err := ws.control(ctx, api.ComponentDescription{
		// Construct a synthetic component description to avoid re-reading after
		// the add. Only the fields needed by control are included.
		// TODO: Store.AddComponent could return a compponent description?
		ID:   id,
		Type: component.Type,
		Spec: component.Spec,
	}, func(lifecycle api.Lifecycle) error {
		_, err := lifecycle.Initialize(ctx, &api.InitializeInput{})
		return err
	}); err != nil {
		return "", err
	}

	// XXX this now double-patches the component to set Initialized timestamp. Optimize?
	if _, err := ws.Store.PatchComponent(ctx, &state.PatchComponentInput{
		ID:          id,
		Initialized: chrono.NowString(ctx),
	}); err != nil {
		return "", fmt.Errorf("modifying component after initialization: %w", err) // XXX this message seems incorrect.
	}

	return id, nil
}

func (ws *Workspace) UpdateComponent(ctx context.Context, input *api.UpdateComponentInput) (*api.UpdateComponentOutput, error) {
	panic("TODO: UpdateComponent") // XXX can implement this now.
}

func (ws *Workspace) updateComponent(ctx context.Context, oldComponent api.ComponentDescription, newComponent manifest.Component) error {
	// TODO: Smart updating, using update lifecycle method.
	name := oldComponent.Name
	id := oldComponent.ID
	if err := ws.deleteComponent(ctx, id); err != nil {
		return fmt.Errorf("delete %q for replacement: %w", name, err)
	}
	if _, err := ws.createComponent(ctx, newComponent); err != nil {
		return fmt.Errorf("adding replacement %q: %w", name, err)
	}
	return nil
}

func (ws *Workspace) RefreshComponents(ctx context.Context, input *api.RefreshComponentsInput) (*api.RefreshComponentsOutput, error) {
	var ids []string
	if input.Refs != nil {
		var err error
		ids, err = ws.resolveRefs(ctx, input.Refs)
		if err != nil {
			return nil, fmt.Errorf("resolving refs: %w", err)
		}
	}

	describeOutput, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		IDs: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("describing components: %w", err)
	}

	refreshTask := ws.TaskTracker.StartTask(ctx, "refresh")
	go func() {
		defer refreshTask.Finish()
		for _, component := range describeOutput.Components {
			refreshTask.Go(component.Name, func(*task.Task) error {
				return ws.refreshComponent(ctx, ws.Store, component)
			})
		}
	}()

	return &api.RefreshComponentsOutput{
		JobID: refreshTask.JobID(),
	}, err
}

func (ws *Workspace) refreshComponent(ctx context.Context, store state.Store, component api.ComponentDescription) error {
	return ws.control(ctx, component, func(lifecycle api.Lifecycle) error {
		_, err := lifecycle.Refresh(ctx, &api.RefreshInput{})
		return err
	})
}

func (ws *Workspace) DisposeComponent(ctx context.Context, input *api.DisposeComponentInput) (*api.DisposeComponentOutput, error) {
	id, err := ws.resolveRef(ctx, input.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolving ref: %w", err)
	}
	err = ws.disposeComponent(ctx, id)
	return &api.DisposeComponentOutput{}, err
}

func (ws *Workspace) resolveRef(ctx context.Context, ref string) (string, error) {
	resolved, err := ws.resolveRefs(ctx, []string{ref})
	if err != nil {
		return "", err
	}
	return resolved[0], nil
}

func (ws *Workspace) resolveRefs(ctx context.Context, refs []string) ([]string, error) {
	resolveOutput, err := ws.Resolve(ctx, &api.ResolveInput{Refs: refs})
	if err != nil {
		return nil, err
	}
	results := make([]string, len(refs))
	for i, id := range resolveOutput.IDs {
		if id == nil {
			return nil, errutil.HTTPErrorf(http.StatusBadRequest, "unresolvable: %q", refs[i])
		}
		results[i] = *id
	}
	return results, nil
}

func (ws *Workspace) disposeComponent(ctx context.Context, id string) error {
	describeOutput, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		IDs: []string{id},
	})
	if err != nil {
		return fmt.Errorf("describing components: %w", err)
	}
	if len(describeOutput.Components) < 1 {
		return fmt.Errorf("no component %q", id)
	}
	component := describeOutput.Components[0]
	return ws.control(ctx, component, func(lifecycle api.Lifecycle) error {
		_, err := lifecycle.Dispose(ctx, &api.DisposeInput{})
		return err
	})
}

func (ws *Workspace) DeleteComponent(ctx context.Context, input *api.DeleteComponentInput) (*api.DeleteComponentOutput, error) {
	id, err := ws.resolveRef(ctx, input.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolving ref: %w", err)
	}
	if err := ws.deleteComponent(ctx, id); err != nil {
		return nil, err
	}
	return &api.DeleteComponentOutput{}, nil
}

func (ws *Workspace) deleteComponent(ctx context.Context, id string) error {
	if err := ws.disposeComponent(ctx, id); err != nil {
		return fmt.Errorf("disposing: %w", err)
	}
	// TODO: Await disposal.
	if _, err := ws.Store.RemoveComponent(ctx, &state.RemoveComponentInput{ID: id}); err != nil {
		return fmt.Errorf("removing from state store: %w", err)
	}
	return nil
}

func (ws *Workspace) DescribeLogs(ctx context.Context, input *api.DescribeLogsInput) (*api.DescribeLogsOutput, error) {
	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		Types: processTypes,
	})
	if err != nil {
		return nil, fmt.Errorf("describing components: %w", err)
	}

	// Find all logs in component hierarchy.
	// TODO: More general handling of log groups, subcomponents, etc.
	var logGroups []string
	var logStreams []string
	streamToGroup := make(map[string]int)
	for _, component := range components.Components {
		// XXX Janky provider inference. See note: [LOG_COMPONENTS].
		var provider string
		switch component.Type {
		case "process":
			provider = "unix"
		case "container":
			provider = "docker"
		}
		for _, streamName := range log.ComponentLogNames(provider, component.ID) {
			streamToGroup[streamName] = len(logGroups)
			logStreams = append(logStreams, streamName)
		}
		logGroups = append(logGroups, component.ID)
	}

	// Initialize output and index by log group name.
	logs := make([]api.LogDescription, len(logGroups))
	for i, logGroup := range logGroups {
		logs[i] = api.LogDescription{
			Name: logGroup,
		}
	}

	// Decorate output with information from the log collector.
	collector := log.CurrentLogCollector(ctx)
	collectorLogs, err := collector.DescribeLogs(ctx, &logd.DescribeLogsInput{
		Names: logStreams,
	})
	if err != nil {
		return nil, err
	}
	for _, collectorLog := range collectorLogs.Logs {
		groupIndex, ok := streamToGroup[collectorLog.Name]
		if !ok {
			continue
		}
		group := &logs[groupIndex]
		group.LastEventAt = combineLastEventAt(group.LastEventAt, collectorLog.LastEventAt)
	}
	return &api.DescribeLogsOutput{Logs: logs}, nil
}

func combineLastEventAt(a, b *string) *string {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if strings.Compare(*a, *b) < 0 {
		return a
	} else {
		return b
	}
}

func (ws *Workspace) GetEvents(ctx context.Context, input *api.GetEventsInput) (*api.GetEventsOutput, error) {
	logGroups := input.Logs
	if logGroups == nil {
		// No filter specified, use all streams.
		logDescriptions, err := ws.DescribeLogs(ctx, &api.DescribeLogsInput{})
		if err != nil {
			return nil, fmt.Errorf("enumerating logs: %w", err)
		}
		logGroups = make([]string, len(logDescriptions.Logs))
		for i, group := range logDescriptions.Logs {
			logGroups[i] = group.Name
		}
	}
	logStreams := make([]string, 0, 2*len(logGroups))
	// Expand log groups in to streams.
	for _, group := range logGroups {
		// Each process acts as a log group combining both stdout and stderr.
		// XXX See note [LOG_COMPONENTS].
		for _, suffix := range []string{"", ":out", ":err"} {
			stream := group + suffix
			logStreams = append(logStreams, stream)
		}
	}

	collector := log.CurrentLogCollector(ctx)
	collectorOutput, err := collector.GetEvents(ctx, &logd.GetEventsInput{
		Logs:   logStreams,
		Cursor: input.Cursor,
		Prev:   input.Prev,
		Next:   input.Next,
	})
	if err != nil {
		return nil, err
	}
	output := api.GetEventsOutput{
		Items:      make([]api.Event, len(collectorOutput.Items)),
		PrevCursor: collectorOutput.PrevCursor,
		NextCursor: collectorOutput.NextCursor,
	}
	for i, collectorEvent := range collectorOutput.Items {
		output.Items[i] = api.Event{
			ID:        collectorEvent.ID,
			Log:       collectorEvent.Log,
			Timestamp: collectorEvent.Timestamp,
			Message:   collectorEvent.Message,
		}
	}
	return &output, nil
}

func (ws *Workspace) Start(ctx context.Context, input *api.StartInput) (*api.StartOutput, error) {
	if err := ws.controlEachProcess(ctx, func(process api.Process) error {
		_, err := process.Start(ctx, &api.StartInput{})
		return err
	}); err != nil {
		return nil, err
	}
	return &api.StartOutput{}, nil
}

func (ws *Workspace) StartComponent(ctx context.Context, input *api.StartComponentInput) (*api.StartComponentOutput, error) {
	id, err := ws.resolveRef(ctx, input.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolving ref: %w", err)
	}

	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		IDs: []string{id},
	})
	if err != nil {
		return nil, fmt.Errorf("fetching component state: %w", err)
	}
	if len(components.Components) != 1 {
		return nil, fmt.Errorf("no state for component: %s", id)
	}
	component := components.Components[0]

	if err := ws.control(ctx, component, func(process api.Process) error {
		_, err := process.Start(ctx, &api.StartInput{})
		return err
	}); err != nil {
		return nil, err
	}
	return &api.StartComponentOutput{}, nil
}

func (ws *Workspace) Stop(ctx context.Context, input *api.StopInput) (*api.StopOutput, error) {
	if err := ws.controlEachProcess(ctx, func(process api.Process) error {
		_, err := process.Stop(ctx, &api.StopInput{})
		return err
	}); err != nil {
		return nil, err
	}
	return &api.StopOutput{}, nil
}

func (ws *Workspace) StopComponent(ctx context.Context, input *api.StopComponentInput) (*api.StopComponentOutput, error) {
	id, err := ws.resolveRef(ctx, input.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolving ref: %w", err)
	}

	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		IDs: []string{id},
	})
	if err != nil {
		return nil, fmt.Errorf("fetching component state: %w", err)
	}
	if len(components.Components) != 1 {
		return nil, fmt.Errorf("no state for component: %s", id)
	}
	component := components.Components[0]

	if err := ws.control(ctx, component, func(process api.Process) error {
		_, err := process.Stop(ctx, &api.StopInput{})
		return err
	}); err != nil {
		return nil, err
	}
	return &api.StopComponentOutput{}, nil
}

func (ws *Workspace) Restart(ctx context.Context, input *api.RestartInput) (*api.RestartOutput, error) {
	if err := ws.controlEachProcess(ctx, func(process api.Process) error {
		_, err := process.Restart(ctx, &api.RestartInput{})
		return err
	}); err != nil {
		return nil, err
	}
	return &api.RestartOutput{}, nil
}

func (ws *Workspace) RestartComponent(ctx context.Context, input *api.RestartComponentInput) (*api.RestartComponentOutput, error) {
	id, err := ws.resolveRef(ctx, input.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolving ref: %w", err)
	}

	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		IDs: []string{id},
	})
	if err != nil {
		return nil, fmt.Errorf("fetching component state: %w", err)
	}
	if len(components.Components) != 1 {
		return nil, fmt.Errorf("no state for component: %s", id)
	}
	component := components.Components[0]

	if err := ws.control(ctx, component, func(process api.Process) error {
		_, err := process.Restart(ctx, &api.RestartInput{})
		return err
	}); err != nil {
		return nil, err
	}
	return &api.RestartComponentOutput{}, nil
}

// TODO: Filter by interface, not concrete type.
var processTypes = []string{"process", "container"}

func (ws *Workspace) DescribeProcesses(ctx context.Context, input *api.DescribeProcessesInput) (*api.DescribeProcessesOutput, error) {
	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		Types: processTypes,
	})
	if err != nil {
		return nil, fmt.Errorf("describing components: %w", err)
	}
	output := api.DescribeProcessesOutput{
		Processes: make([]api.ProcessDescription, 0, len(components.Components)),
	}
	for _, component := range components.Components {
		// XXX Violates component state encapsulation.
		switch component.Type {
		case "process":
			var state process.State
			if err := jsonutil.UnmarshalString(component.State, &state); err != nil {
				// TODO: log error.
				fmt.Printf("unmarshalling process state: %v\n", err)
				continue
			}
			process := api.ProcessDescription{
				ID:       component.ID,
				Name:     component.Name,
				Provider: "unix",
				EnvVars:  state.FullEnvironment,
			}

			proc, err := psprocess.NewProcess(int32(state.Pid))
			if err == nil {
				process.Running, err = proc.IsRunning()
				if err != nil {
					return nil, err
				}

				memoryInfo, err := proc.MemoryInfo()
				if err != nil {
					return nil, err
				}

				process.ResidentMemory = memoryInfo.RSS

				connections, err := proc.Connections()
				if err != nil {
					return nil, err
				}

				var ports []uint32
				for _, conn := range connections {
					if conn.Laddr.Port != 0 {
						ports = append(ports, conn.Laddr.Port)
					}
				}
				process.Ports = ports

				process.CreateTime, err = proc.CreateTime()
				if err != nil {
					return nil, err
				}

				children, err := proc.Children()
				if err == nil {
					var childrenExecutables []string
					for _, child := range children {
						exe, err := child.Exe()
						if err != nil {
							return nil, err
						}
						childrenExecutables = append(childrenExecutables, exe)
					}
					process.ChildrenExecutables = childrenExecutables
				}

				process.CPUPercent, err = proc.CPUPercent()
				if err != nil {
					return nil, err
				}
			}
			output.Processes = append(output.Processes, process)
		case "container":
			var state struct {
				Running bool `json:"running"`
			}
			if err := jsonutil.UnmarshalString(component.State, &state); err != nil {
				// TODO: log error.
				fmt.Printf("unmarshalling container state: %v\n", err)
				continue
			}
			process := api.ProcessDescription{
				ID:       component.ID,
				Name:     component.Name,
				Provider: "docker",
				Running:  state.Running,
			}
			output.Processes = append(output.Processes, process)
		}
	}
	return &output, nil
}

func (ws *Workspace) DescribeVolumes(ctx context.Context, input *api.DescribeVolumesInput) (*api.DescribeVolumesOutput, error) {
	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		Types: []string{"volume"},
	})
	if err != nil {
		return nil, fmt.Errorf("describing components: %w", err)
	}
	output := api.DescribeVolumesOutput{
		Volumes: make([]api.VolumeDescription, 0, len(components.Components)),
	}
	for _, component := range components.Components {
		volume := api.VolumeDescription{
			ID:   component.ID,
			Name: component.Name,
		}
		output.Volumes = append(output.Volumes, volume)
	}
	return &output, nil
}

func (ws *Workspace) DescribeNetworks(ctx context.Context, input *api.DescribeNetworksInput) (*api.DescribeNetworksOutput, error) {
	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		Types: []string{"network"},
	})
	if err != nil {
		return nil, fmt.Errorf("describing components: %w", err)
	}
	output := api.DescribeNetworksOutput{
		Networks: make([]api.NetworkDescription, 0, len(components.Components)),
	}
	for _, component := range components.Components {
		network := api.NetworkDescription{
			ID:   component.ID,
			Name: component.Name,
		}
		output.Networks = append(output.Networks, network)
	}
	return &output, nil
}

func (ws *Workspace) controlEachProcess(ctx context.Context, f interface{}) error {
	components, err := ws.DescribeComponents(ctx, &api.DescribeComponentsInput{
		Types: processTypes,
	})
	if err != nil {
		return fmt.Errorf("describing components: %w", err)
	}
	for _, component := range components.Components {
		if err := ws.control(ctx, component, f); err != nil {
			return fmt.Errorf("controlling %q: %w", component.ID, err)
		}
	}
	return nil
}

func (ws *Workspace) control(ctx context.Context, desc api.ComponentDescription, f interface{}) error {
	ctrl := ws.newController(ctx, desc.Type)
	if err := ctrl.InitResource(desc.ID, desc.Spec, desc.State); err != nil {
		return err
	}
	fV := reflect.ValueOf(f)
	ctrlV := reflect.ValueOf(ctrl)
	argT := fV.Type().In(0)
	if !ctrlV.Type().AssignableTo(argT) {
		return fmt.Errorf("%q controller does not implement %s", desc.Type, argT)
	}
	results := fV.Call([]reflect.Value{ctrlV})
	fErr, _ := results[0].Interface().(error)
	// Try to save state even if f fails.
	newState, err := ctrl.MarshalState()
	if err == nil {
		_, err = ws.Store.PatchComponent(ctx, &state.PatchComponentInput{
			ID:    desc.ID,
			State: newState,
		})
	}
	if fErr != nil {
		return fErr
	}
	return err
}