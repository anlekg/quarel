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
  pinServer: (host: string, sid: string) => ipcRenderer.invoke('tls:pin', host, sid),
  checkServer: (host: string, port: number, sid: string) => ipcRenderer.invoke('tls:check', host, port, sid),
  screenSources: () => ipcRenderer.invoke('screen:sources'),
  chooseScreenSource: (id: string) => ipcRenderer.invoke('screen:choose', id),
})
