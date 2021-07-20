// Generated file. DO NOT EDIT.

package client

import (
	"context"

	"github.com/deref/exo/exod/api"
	josh "github.com/deref/exo/josh/client"
)

type Workspace struct {
	client *josh.Client
}

var _ api.Workspace = (*Workspace)(nil)

func GetWorkspace(client *josh.Client) *Workspace {
	return &Workspace{
		client: client,
	}
}

func (c *Workspace) Describe(ctx context.Context, input *api.DescribeInput) (output *api.DescribeOutput, err error) {
	err = c.client.Invoke(ctx, "describe", input, &output)
	return
}

func (c *Workspace) Destroy(ctx context.Context, input *api.DestroyInput) (output *api.DestroyOutput, err error) {
	err = c.client.Invoke(ctx, "destroy", input, &output)
	return
}

func (c *Workspace) Apply(ctx context.Context, input *api.ApplyInput) (output *api.ApplyOutput, err error) {
	err = c.client.Invoke(ctx, "apply", input, &output)
	return
}

func (c *Workspace) ApplyProcfile(ctx context.Context, input *api.ApplyProcfileInput) (output *api.ApplyProcfileOutput, err error) {
	err = c.client.Invoke(ctx, "apply-procfile", input, &output)
	return
}

func (c *Workspace) Refresh(ctx context.Context, input *api.RefreshInput) (output *api.RefreshOutput, err error) {
	err = c.client.Invoke(ctx, "refresh", input, &output)
	return
}

func (c *Workspace) Resolve(ctx context.Context, input *api.ResolveInput) (output *api.ResolveOutput, err error) {
	err = c.client.Invoke(ctx, "resolve", input, &output)
	return
}

func (c *Workspace) DescribeComponents(ctx context.Context, input *api.DescribeComponentsInput) (output *api.DescribeComponentsOutput, err error) {
	err = c.client.Invoke(ctx, "describe-components", input, &output)
	return
}

func (c *Workspace) CreateComponent(ctx context.Context, input *api.CreateComponentInput) (output *api.CreateComponentOutput, err error) {
	err = c.client.Invoke(ctx, "create-component", input, &output)
	return
}

func (c *Workspace) UpdateComponent(ctx context.Context, input *api.UpdateComponentInput) (output *api.UpdateComponentOutput, err error) {
	err = c.client.Invoke(ctx, "update-component", input, &output)
	return
}

func (c *Workspace) RefreshComponent(ctx context.Context, input *api.RefreshComponentInput) (output *api.RefreshComponentOutput, err error) {
	err = c.client.Invoke(ctx, "refresh-component", input, &output)
	return
}

func (c *Workspace) DisposeComponent(ctx context.Context, input *api.DisposeComponentInput) (output *api.DisposeComponentOutput, err error) {
	err = c.client.Invoke(ctx, "dispose-component", input, &output)
	return
}

func (c *Workspace) DeleteComponent(ctx context.Context, input *api.DeleteComponentInput) (output *api.DeleteComponentOutput, err error) {
	err = c.client.Invoke(ctx, "delete-component", input, &output)
	return
}

func (c *Workspace) DescribeLogs(ctx context.Context, input *api.DescribeLogsInput) (output *api.DescribeLogsOutput, err error) {
	err = c.client.Invoke(ctx, "describe-logs", input, &output)
	return
}

func (c *Workspace) GetEvents(ctx context.Context, input *api.GetEventsInput) (output *api.GetEventsOutput, err error) {
	err = c.client.Invoke(ctx, "get-events", input, &output)
	return
}

func (c *Workspace) Start(ctx context.Context, input *api.StartInput) (output *api.StartOutput, err error) {
	err = c.client.Invoke(ctx, "start", input, &output)
	return
}

func (c *Workspace) Stop(ctx context.Context, input *api.StopInput) (output *api.StopOutput, err error) {
	err = c.client.Invoke(ctx, "stop", input, &output)
	return
}

func (c *Workspace) DescribeProcesses(ctx context.Context, input *api.DescribeProcessesInput) (output *api.DescribeProcessesOutput, err error) {
	err = c.client.Invoke(ctx, "describe-processes", input, &output)
	return
}