// Signs the update descriptions (latest.yml, latest-linux.yml) of a built
// release with the Ed25519 release key: node scripts/sign-release.mjs [dir]
// Key: $QUAREL_RELEASE_KEY (PEM file), default ~/.config/quarel-release/update-signing.pem.
import { createPrivateKey, createPublicKey, sign } from 'node:crypto'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

const dir = process.argv[2] ?? 'release'
const keyFile = process.env.QUAREL_RELEASE_KEY ?? join(homedir(), '.config/quarel-release/update-signing.pem')
const key = createPrivateKey(readFileSync(keyFile))
// The key must be the one the app trusts (electron/releasesig.ts), or nobody could install the release.
const pub = createPublicKey(key).export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64')
const trusted = /RELEASE_KEY = '([^']+)'/.exec(readFileSync(new URL('../electron/releasesig.ts', import.meta.url), 'utf8'))?.[1]
if (pub !== trusted) throw new Error('cette clé n’est pas la clé de publication attendue par l’application (' + pub + ')')
let n = 0
for (const f of ['latest.yml', 'latest-linux.yml', 'latest-mac.yml']) {
  const p = join(dir, f)
  if (!existsSync(p)) continue
  const sig = sign(null, Buffer.concat([Buffer.from('quarel-update-v1\0'), readFileSync(p)]), key)
  writeFileSync(p + '.sig', sig.toString('base64') + '\n')
  console.log('signé :', f)
  n++
}
if (!n) throw new Error('aucune description de version dans ' + dir)
