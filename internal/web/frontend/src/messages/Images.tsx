import { useLayoutEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { imageTypes } from './model';
import type { ImageData } from './model';
import { imageURL, loadImage, scopeKey, scopeOf } from './imageTransport';
import type { ImageRead } from './imageTransport';
function ImageTile({image, position, root, messageID}: {image: ImageData; position: number; root: HTMLElement | null; messageID: string}) {
  const tile = useRef<HTMLDivElement>(null), job = useRef<ImageRead | undefined>(undefined);
  const scope = scopeOf(root), scopeIdentity = scopeKey(scope);
  const url = imageURL(image.url, scope, messageID, image.index, image.mime, location.origin);
  const pending = imageTypes.has(image.mime) && image.index >= 0 && image.url === '';
  const identity = JSON.stringify([scopeIdentity, image.index, image.mime, url, pending]);
  const [loaded, setLoaded] = useState({identity, state: url ? 'queued' : pending ? 'pending' : 'unavailable', src: ''});
  const state = loaded.identity === identity ? loaded : {identity, state: url ? 'queued' : pending ? 'pending' : 'unavailable', src: ''};
  useLayoutEffect(() => {
    if (!url) return;
    let retired = false;
    job.current = loadImage(url, image.mime, () => !retired && !!tile.current?.isConnected && scopeKey(scopeOf(root)) === scopeIdentity,
      (status, src) => { if (!retired) flushSync(() => setLoaded({identity, state: status, src})); });
    return () => { retired = true; job.current?.release(); job.current = undefined; };
  }, [identity, root, url, image.mime, scopeIdentity]);
  return <div ref={tile} className="message-image" data-image-index={image.index} data-image-mime={image.mime} data-image-state={state.state}>
    <img key={identity} className="message-image-preview" width={96} height={96} decoding="async" loading="eager" alt={`Attached image ${position + 1}`} data-image-url={url || undefined} src={state.src || undefined} hidden={state.state !== 'loaded'} onLoad={() => job.current?.decoded(true)} onError={() => job.current?.decoded(false)}/>
    <span className="message-image-fallback" hidden={state.state === 'loaded'}>{state.state === 'pending' ? 'Image pending' : state.state === 'unavailable' ? 'Image unavailable' : 'Loading image'}</span>
  </div>;
}
export function Images({images, root, messageID}: {images: ImageData[]; root: HTMLElement | null; messageID: string}) {
  if (!images.length) return null;
  return <div className="message-images" role="group" aria-label="Attached images">{images.map((image, position) => <ImageTile key={position} image={image} position={position} root={root} messageID={messageID}/>)}</div>;
}
