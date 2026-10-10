import { useState } from 'react';
import { initials, useAvatarUrl } from '../lib/avatars';

/**
 * A person: their stored picture when one has been fetched, their initials
 * otherwise. Square, like everything else in this interface.
 */
export const Avatar = ({ address, name, size = 40, className = '' }: {
  address?: string | null; name?: string | null; size?: number; className?: string;
}) => {
  const url = useAvatarUrl(address);
  const [failed, setFailed] = useState<string | null>(null);
  const style = { width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.4)) };
  if (url && failed !== url) {
    return (
      <img
        src={url}
        alt=""
        style={style}
        className={`shrink-0 object-cover bg-muted ${className}`}
        onError={() => setFailed(url)}
      />
    );
  }
  return (
    <span style={style} aria-hidden="true"
      className={`shrink-0 inline-flex items-center justify-center bg-primary/15 text-primary font-semibold ${className}`}>
      {initials(name, address)}
    </span>
  );
};
