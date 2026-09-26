// Desktop app release shown on the site: the version of client/package.json,
// which scripts/release.mjs sets when it publishes to app.quarel.app/updates.
import client from '../../../client/package.json'

export const version: string = client.version

export const webApp = 'https://app.quarel.app'
export const github = 'https://github.com/anlekg/quarel'
const feed = 'https://app.quarel.app/updates/'

export const downloads = {
  windows: feed + 'Quarel-Setup-' + version + '.exe',
  appimage: feed + 'Quarel-' + version + '-x86_64.AppImage',
  deb: feed + 'Quarel-' + version + '-amd64.deb',
}
