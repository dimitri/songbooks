// image-shrink.js — a logo picked for upload is made to fit before it goes:
// the server takes PNG, JPEG or WebP up to 2 MB and keeps the logo fitted
// into 512 px anyway (internal/brand/logo.go), so a photo straight off a
// camera or a 3000 px export is scaled down here (longest side 1024 px)
// rather than refused. Transparency is kept (PNG); a file still too big
// as PNG goes as WebP, then JPEG. A file that already fits is sent as is.
//
// ImageShrink.fit(file) → Promise<File>; it rejects only when the browser
// can't read the image at all (the caller then sends the file unchanged
// and lets the server say why).
(function (root) {
  if (root.ImageShrink) return;
  const MAX_BYTES = 2 * 1024 * 1024;
  const MAX_SIDE = 1024;
  const KEEP = /^image\/(png|jpeg|webp)$/;

  // The size to draw at: the longest side at most max, never enlarged.
  function fitSize(w, h, max) {
    const s = Math.min(1, max / Math.max(w, h));
    return { w: Math.max(1, Math.round(w * s)), h: Math.max(1, Math.round(h * s)) };
  }

  async function decode(file) {
    if (root.createImageBitmap) {
      try { return await createImageBitmap(file); } catch (e) { /* Safari: fall back to an <img> */ }
    }
    const url = URL.createObjectURL(file);
    try {
      const img = new Image();
      img.decoding = 'async';
      img.src = url;
      await img.decode();
      return img;
    } finally {
      URL.revokeObjectURL(url);
    }
  }

  const blobOf = (canvas, type, q) => new Promise((ok) => canvas.toBlob(ok, type, q));

  async function fit(file) {
    if (file.size <= MAX_BYTES && KEEP.test(file.type)) return file;
    const img = await decode(file);
    const w0 = img.width || img.naturalWidth, h0 = img.height || img.naturalHeight;
    if (!w0 || !h0) throw new Error('unreadable image');
    const { w, h } = fitSize(w0, h0, MAX_SIDE);
    const canvas = document.createElement('canvas');
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext('2d');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(img, 0, 0, w, h);
    if (img.close) img.close();
    const base = (file.name || 'logo').replace(/\.[^.]+$/, '');
    for (const [type, q, ext] of [['image/png', undefined, 'png'], ['image/webp', 0.92, 'webp'], ['image/jpeg', 0.9, 'jpg']]) {
      const b = await blobOf(canvas, type, q);
      if (b && b.type === type && b.size <= MAX_BYTES) return new File([b], `${base}.${ext}`, { type });
    }
    throw new Error('still too large');
  }

  root.ImageShrink = { fit, fitSize, MAX_BYTES, MAX_SIDE };
})(typeof window !== 'undefined' ? window : globalThis);
