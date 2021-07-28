import type { GetVersionResponse } from './kernel/types';
import type { LogsResponse } from './logs/types';
import type {
  CreateProcessResponse,
  ProcessDescription,
} from './process/types';

interface IdleRequest {
  stage: 'idle';
}

interface PendingRequest {
  stage: 'pending';
}

interface ErrorResponse {
  stage: 'error';
  message: string;
}

interface SuccessResponse<T> {
  stage: 'success';
  data: T;
}

interface RefetchingResponse<T> {
  stage: 'refetching';
  data: T;
}

export type RemoteData<T> =
  | IdleRequest
  | PendingRequest
  | ErrorResponse
  | SuccessResponse<T>
  | RefetchingResponse<T>;

export const notRequested = <T>(): RemoteData<T> => ({ stage: 'idle' });
export const pendingRequest = <T>(): RemoteData<T> => ({ stage: 'pending' });
export const errorResponse = <T>(message: string): RemoteData<T> => ({
  stage: 'error',
  message,
});
export const successResponse = <T>(data: T): RemoteData<T> => ({
  stage: 'success',
  data,
});
export const refetchingResponse = <T>(prev: T): RemoteData<T> => ({
  stage: 'refetching',
  data: prev,
});

type HasData<T> = SuccessResponse<T> | RefetchingResponse<T>;
// TODO: Should idle be considered unresolved?
type IsUnresolved<T> = IdleRequest | PendingRequest | RefetchingResponse<T>;
type IsResolved<T> = ErrorResponse | SuccessResponse<T>;

export const hasData = <T>(r: RemoteData<T>): r is HasData<T> =>
  r.stage === 'success' || r.stage === 'refetching';

export const IsUnresolved = <T>(r: RemoteData<T>): r is IsUnresolved<T> =>
  r.stage === 'idle' || r.stage === 'pending' || r.stage === 'refetching';

export const IsResolved = <T>(r: RemoteData<T>): r is IsResolved<T> =>
  r.stage === 'error' || r.stage === 'success';

export interface PaginationParams {
  cursor: string | null;
  prev?: number;
  next?: number;
}

const baseUrl = 'http://localhost:4000/_exo';

const apiUrl = (path: string, query: Record<string, string>) => {
  let qs = '';
  let sep = '?';
  for (const [key, value] of Object.entries(query)) {
    qs += sep;
    sep = '&';
    qs += encodeURIComponent(key);
    qs += '=';
    qs += encodeURIComponent(value);
  }
  return baseUrl + path + qs;
};

const isErrorLike = (x: any): x is { message: string } => {
  return x != null && 'message' in x && typeof x.message === 'string';
};

export class APIError extends Error {
  constructor(public readonly httpStatus: number, message: string) {
    super(message);
  }
}

export const isClientError = (err: Error): err is APIError =>
  err instanceof APIError && 400 <= err.httpStatus && err.httpStatus < 500;

const responseToError = async (res: Response): Promise<Error | null> => {
  if (200 <= res.status && res.status < 300) {
    return null;
  }
  const text = await res.text();
  let json: unknown;
  try {
    json = JSON.parse(text);
  } catch (_: unknown) {
    json = text;
  }
  if (!isErrorLike(json)) {
    return new Error(`malformed error from server: ${JSON.stringify(json)}`);
  }
  return new APIError(res.status, json.message);
};

const rpc = async (
  path: string,
  query: Record<string, string>,
  data?: unknown,
): Promise<unknown> => {
  const res = await fetch(apiUrl(path, query), {
    method: 'POST',
    headers: {
      accept: 'application/json',
      'content-type': 'application/json',
    },
    ...(data
      ? {
          body: JSON.stringify(data),
        }
      : {}),
  });
  const err = await responseToError(res);
  if (err !== null) {
    throw err;
  }
  return await res.json();
};

export interface ProcessSpec {
  directory?: string;
  program: string;
  arguments: string[];
  environment?: Record<string, string>;
}

export interface WorkspaceDescription {
  id: string;
  root: string;
}

export const api = (() => {
  const kernel = (() => {
    const invoke = (method: string, data?: unknown) =>
      rpc(`/kernel/${method}`, {}, data);
    return {
      async describeWorkspaces(): Promise<WorkspaceDescription[]> {
        const { workspaces } = (await invoke('describe-workspaces', {})) as any;
        return workspaces;
      },
      async createWorkspace(root: string): Promise<string> {
        const { id } = (await invoke('create-workspace', { root })) as any;
        return id;
      },

      async getVersion(): Promise<GetVersionResponse> {
        return (await invoke('get-version', {})) as any;
      },

      async upgrade(): Promise<void> {
        return (await invoke('upgrade', {})) as any;
      },
    };
  })();

  const workspace = (id: string) => {
    const invoke = (method: string, data?: unknown) =>
      rpc(`/workspace/${method}`, { id }, data);
    return {
      async describeProcesses(): Promise<ProcessDescription[]> {
        const { processes } = (await invoke('describe-processes')) as any;
        return processes;
      },

      async apply() {
        await invoke('apply', {});
      },

      async createProcess(
        name: string,
        spec: ProcessSpec,
      ): Promise<CreateProcessResponse> {
        return (await invoke('create-component', {
          name,
          type: 'process',
          spec: JSON.stringify(spec),
        })) as CreateProcessResponse;
      },

      async startProcess(ref: string): Promise<void> {
        await invoke('start-component', { ref });
      },

      async stopProcess(ref: string): Promise<void> {
        await invoke('stop-component', { ref });
      },

      async deleteComponent(ref: string): Promise<void> {
        await invoke('delete-component', { ref });
      },

      async refreshAllProcesses(): Promise<void> {
        await invoke('refresh-all-components');
      },

      async getEvents(
        logs: string[],
        pagination?: PaginationParams,
      ): Promise<LogsResponse> {
        return (await invoke('get-events', {
          logs,
          ...pagination,
        })) as LogsResponse;
      },
    };
  };

  return {
    kernel,
    workspace,
  };
})();
