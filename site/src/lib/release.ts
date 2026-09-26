// Desktop app release shown on the site: the version of client/package.json,
// which scripts/release.mjs sets when it publishes to app.quarel.app/updates.
import client from '../../../client/package.json'
import serversVersion from '../../../packaging/windows/VERSION?raw'

export const version: string = client.version

export const webApp = 'https://app.quarel.app'
export const github = 'https://github.com/anlekg/quarel'
const feed = 'https://app.quarel.app/updates/'

export const downloads = {
  windows: feed + 'Quarel-Setup-' + version + '.exe',
  appimage: feed + 'Quarel-' + version + '-x86_64.AppImage',
  deb: feed + 'Quarel-' + version + '-amd64.deb',
}

// Windows installers of the servers (packaging/windows/release.sh → quarel.app/telechargements).
export const serverVersion: string = serversVersion.trim()
const dl = 'https://quarel.app/telechargements/'
export const serverDownloads = {
  community: dl + 'Quarel-Serveur-Setup-' + serverVersion + '.exe',
  identity: dl + 'Quarel-Identite-Setup-' + serverVersion + '.exe',
  sums: dl + 'SHA256SUMS',
}
