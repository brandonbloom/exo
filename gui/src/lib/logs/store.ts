import { writable } from 'svelte/store';
import { api, notRequested, pendingRequest, refetchingResponse, RemoteData, successResponse } from '../api';
import type { LogEvent } from './types';
export interface LogsStore {
  logs: string[];
  events: RemoteData<LogEvent[]>;
  logBufferSize: number;
}

let lastCursor: string | null = null;

export const logsStore = writable<LogsStore>({
  logs: [],
  events: notRequested(),
  logBufferSize: 1000,
});

export const refreshLogs = async (fromStart = false) => {
  if (fromStart) {
    lastCursor = null;
  }

  let matchProcs = [];
  logsStore.update((value) => {
    matchProcs = value.logs;
    switch (value.events.stage) {
      case 'idle':
        return {
          ...value,
          events: pendingRequest(),
        };
      case 'pending':
        // TODO: Prevent re-fetch of pending request.
        return value;
      case 'error':
        return {
          ...value,
          events: pendingRequest(),
        };
      case 'success':
        return {
          ...value,
          events: refetchingResponse(fromStart ? [] : value.events.data),
        };
    }
  });

  const newEvents = await api.getEvents(matchProcs, {
    type: 'after-cursor',
    cursor: fromStart ? null : lastCursor,
  });

  lastCursor = newEvents.cursor;
  logsStore.update(value => {
    let prevEvents: LogEvent[] = [];
    if (value.events.stage === 'success' || value.events.stage === 'refetching') {
      prevEvents = value.events.data;
    }
    const allEvents = [...prevEvents, ...newEvents.events];

    return {
      ...value,
      events: successResponse(allEvents.slice(allEvents.length-value.logBufferSize)),
    }
  });
};

const addToList = <T>(xs: T[], x: T): T[] => {
  if (xs.includes(x)) {
    return xs;
  }
  return [...xs, x];
};

const removeFromList = <T>(xs: T[], x: T): T[] =>
  xs.filter(elem => elem !== x);

export const setLogVisibility = (processId: string, isVisible: boolean) => {
  // XXX: This is broken because `value.log` no longer matches the process name.
  logsStore.update(value => ({
    ...value,
    logs: isVisible ? addToList(value.logs, processId) : removeFromList(value.logs, processId),
  }));
  refreshLogs(true);
};