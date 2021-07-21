import Home from './pages/Home.svelte'
import Workspace from './pages/Workspace.svelte'
import NewWorkspace from './pages/NewWorkspace.svelte'
import NewProcess from './pages/NewProcess.svelte'
import NotFound from './pages/NotFound.svelte'

export default {
  '/': Home,
  '/new-workspace': NewWorkspace,
  '/workspaces/:workspace': Workspace,
  '/workspaces/:workspace/new-process': NewProcess,
  '*': NotFound,
};