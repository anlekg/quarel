// Images sent to a server (avatars, banners, backgrounds): kept as they are
// when small enough, else reduced in the app (WebP) so the upload fits.
export async function prepareImage(file: Blob, maxBytes: number, maxSide: number): Promise<Blob> {
  const bitmap = await createImageBitmap(file).catch(() => null)
  if (!bitmap) throw new Error('image')
  if (file.size <= maxBytes && bitmap.width <= maxSide * 2 && bitmap.height <= maxSide * 2) return file
  const scale = Math.min(1, maxSide / Math.max(bitmap.width, bitmap.height))
  const canvas = document.createElement('canvas')
  canvas.width = Math.round(bitmap.width * scale)
  canvas.height = Math.round(bitmap.height * scale)
  canvas.getContext('2d')!.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
  return new Promise((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('image'))), 'image/webp', 0.88))
}

export const imageLimits = {
  avatar: [1 << 20, 512],
  banner: [2 << 20, 1200],
  background: [4 << 20, 2560],
} as const
