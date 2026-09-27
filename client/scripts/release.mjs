// Builds, signs and publishes a desktop app release:
//   node scripts/release.mjs 0.2.0
// → Windows installer + Linux AppImage/.deb in release/, update descriptions
// signed (sign-release.mjs), then copied to $QUAREL_UPDATES_DIR (the folder
// served at app.quarel.app/updates). Descriptions last: clients never see a
// version whose files are not there yet. Keeps the files of 3 versions.
import { execSync } from 'node:child_process'
import { copyFileSync, mkdirSync, readdirSync, renameSync, rmSync } from 'node:fs'
import { join } from 'node:path'

const version = process.argv[2]
if (!/^\d+\.\d+\.\d+$/.test(version ?? '')) throw new Error('usage : node scripts/release.mjs <version, ex. 0.2.0>')
const dest = process.env.QUAREL_UPDATES_DIR
if (!dest) throw new Error('QUAREL_UPDATES_DIR : dossier servi à app.quarel.app/updates (voir local.mk)')
const run = (cmd) => execSync(cmd, { stdio: 'inherit' })

run(`npm version ${version} --no-git-tag-version --allow-same-version`)
rmSync('release', { recursive: true, force: true })
run('npm run build')
// Published builds also refuse --inspect (the fuse is left on in electron-builder.yml
// so that test builds can be driven by Playwright, which needs it).
run('npx electron-builder --linux --publish never -c.electronFuses.enableNodeCliInspectArguments=false')
// Windows also checks app.asar against the hash embedded in Quarel.exe (the app refuses to start
// if its code was changed after install). Electron supports it on Windows and macOS only.
run('npx electron-builder --win --publish never -c.electronFuses.enableNodeCliInspectArguments=false -c.electronFuses.enableEmbeddedAsarIntegrityValidation=true')
run('node scripts/sign-release.mjs release')

mkdirSync(dest, { recursive: true })
const files = readdirSync('release').filter((f) => f.includes(version) && /\.(exe|blockmap|AppImage|deb)$/.test(f))
for (const f of files) copyFileSync(join('release', f), join(dest, f))
for (const f of readdirSync('release').filter((f) => /^latest.*\.yml(\.sig)?$/.test(f)).sort((a, b) => b.length - a.length)) {
  // .sig before .yml, each swapped in at once (never half-written).
  copyFileSync(join('release', f), join(dest, '.' + f + '.tmp'))
  renameSync(join(dest, '.' + f + '.tmp'), join(dest, f))
}
// Old versions: keep the 3 latest (differential updates use the previous block maps).
const versions = [...new Set(readdirSync(dest).map((f) => /-(\d+\.\d+\.\d+)[-.]/.exec(f)?.[1]).filter(Boolean))]
  .sort((a, b) => b.localeCompare(a, undefined, { numeric: true }))
for (const old of versions.slice(3)) for (const f of readdirSync(dest).filter((f) => f.includes('-' + old))) rmSync(join(dest, f))
console.log(`Version ${version} publiée dans ${dest} :`, files.join(', '))
