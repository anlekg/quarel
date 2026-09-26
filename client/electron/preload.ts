// The only API the web UI gets from the desktop shell.
import { contextBridge, ipcRenderer } from 'electron'

contextBridge.exposeInMainWorld('quarelDesktop', {
  secrets: {
    get: (key: string) => ipcRenderer.invoke('secrets:get', key),
    set: (key: string, value: string) => ipcRenderer.invoke('secrets:set', key, value),
    delete: (key: string) => ipcRenderer.invoke('secrets:delete', key),
  },
  info: () => ipcRenderer.invoke('app:info'),
  vault: {
    get: (key: string) => ipcRenderer.invoke('vault:get', key),
    set: (key: string, value: string) => ipcRenderer.invoke('vault:set', key, value),
  },
  files: {
    get: (id: string) => ipcRenderer.invoke('files:get', id),
    put: (id: string, data: Uint8Array) => ipcRenderer.invoke('files:put', id, data),
    delete: (id: string) => ipcRenderer.invoke('files:delete', id),
  },
  takeInvite: () => ipcRenderer.invoke('invite:take'),
  updates: {
    state: () => ipcRenderer.invoke('update:state'),
    check: () => ipcRenderer.invoke('update:check'),
    install: () => ipcRenderer.invoke('update:install'),
    onState: (cb: (s: unknown) => void) => {
      const l = (_e: unknown, s: unknown) => cb(s)
      ipcRenderer.on('update:state', l)
      return () => ipcRenderer.removeListener('update:state', l)
    },
  },
  onInvite: (cb: (link: string) => void) => {
    const l = (_e: unknown, link: string) => cb(link)
    ipcRenderer.on('invite:open', l)
    return () => ipcRenderer.removeListener('invite:open', l)
  },
  pinServer: (host: string, sid: string) => ipcRenderer.invoke('tls:pin', host, sid),
  checkServer: (host: string, port: number, sid: string) => ipcRenderer.invoke('tls:check', host, port, sid),
  screenSources: () => ipcRenderer.invoke('screen:sources'),
  chooseScreenSource: (id: string) => ipcRenderer.invoke('screen:choose', id),
})
