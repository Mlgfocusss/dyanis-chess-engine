import { useEffect, useRef, useState } from "react";

/** Tracks a container's width so the board can be responsive. */
export function useElementWidth() {
  const ref = useRef(null);
  const [width, setWidth] = useState(480);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setWidth(Math.floor(entry.contentRect.width));
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  return [ref, width];
}