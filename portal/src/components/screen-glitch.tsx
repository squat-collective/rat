"use client";

import { useEffect, useState, useCallback, useMemo, useRef } from "react";

const GLITCH_DURATION = 800;
const TEAR_COUNT = 6;

function randomBetween(min: number, max: number) {
  return Math.random() * (max - min) + min;
}

function GlitchOverlayEl({ active }: { active: boolean }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const frameRef = useRef<number>(0);
  const tearsRef = useRef<Array<{ y: number; h: number; dx: number; speed: number }>>([]);
  const startRef = useRef(0);

  useEffect(() => {
    if (!active) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;

    // generate random horizontal tear lines
    tearsRef.current = Array.from({ length: TEAR_COUNT }, () => ({
      y: Math.random() * canvas.height,
      h: randomBetween(1, 6),
      dx: randomBetween(-30, 30),
      speed: randomBetween(0.5, 3),
    }));

    startRef.current = performance.now();

    const animate = (now: number) => {
      const elapsed = now - startRef.current;
      const progress = Math.min(elapsed / GLITCH_DURATION, 1);
      const intensity = progress < 0.3 ? progress / 0.3 : 1 - (progress - 0.3) / 0.7;

      ctx.clearRect(0, 0, canvas.width, canvas.height);

      // --- static noise blocks ---
      if (Math.random() < intensity * 0.7) {
        const blockCount = Math.floor(randomBetween(2, 8) * intensity);
        for (let i = 0; i < blockCount; i++) {
          const bx = Math.random() * canvas.width;
          const by = Math.random() * canvas.height;
          const bw = randomBetween(20, 200) * intensity;
          const bh = randomBetween(2, 8);
          ctx.fillStyle = `hsla(${Math.random() > 0.5 ? 142 : 180}, 100%, 50%, ${randomBetween(0.05, 0.2) * intensity})`;
          ctx.fillRect(bx, by, bw, bh);
        }
      }

      // --- horizontal tear lines ---
      for (const tear of tearsRef.current) {
        tear.y = (tear.y + tear.speed) % canvas.height;
        const offset = tear.dx * intensity * (Math.random() > 0.3 ? 1 : -1);

        // displaced slice — grab a strip and redraw it shifted
        ctx.fillStyle = `hsla(0, 100%, 50%, ${0.08 * intensity})`;
        ctx.fillRect(0, tear.y, canvas.width, tear.h);

        // cyan ghost on the offset side
        ctx.fillStyle = `hsla(180, 100%, 50%, ${0.06 * intensity})`;
        ctx.fillRect(offset, tear.y - 1, canvas.width, tear.h * 0.5);
      }

      // --- RGB split bars (abrupt horizontal displacement) ---
      if (Math.random() < intensity * 0.5) {
        const barY = Math.random() * canvas.height;
        const barH = randomBetween(15, 80) * intensity;
        const shift = randomBetween(3, 15) * intensity * (Math.random() > 0.5 ? 1 : -1);

        // red channel
        ctx.fillStyle = `hsla(0, 100%, 50%, ${0.12 * intensity})`;
        ctx.fillRect(shift, barY, canvas.width, barH);
        // cyan channel (opposite direction)
        ctx.fillStyle = `hsla(180, 100%, 50%, ${0.1 * intensity})`;
        ctx.fillRect(-shift, barY + 2, canvas.width, barH);
      }

      // --- scanline interference ---
      ctx.fillStyle = `hsla(142, 72%, 45%, ${0.02 * intensity})`;
      for (let sy = 0; sy < canvas.height; sy += 4) {
        if (Math.random() < 0.3 * intensity) {
          ctx.fillRect(0, sy, canvas.width, 1);
        }
      }

      // --- brief full-screen flash at peak ---
      if (progress > 0.1 && progress < 0.2 && Math.random() < 0.4) {
        ctx.fillStyle = `hsla(142, 72%, 45%, ${0.06})`;
        ctx.fillRect(0, 0, canvas.width, canvas.height);
      }

      if (progress < 1) {
        frameRef.current = requestAnimationFrame(animate);
      }
    };

    frameRef.current = requestAnimationFrame(animate);
    return () => cancelAnimationFrame(frameRef.current);
  }, [active]);

  if (!active) return null;

  return (
    <canvas
      ref={canvasRef}
      className="fixed inset-0 z-[9997] pointer-events-none"
      style={{ mixBlendMode: "screen" }}
    />
  );
}

export function useScreenGlitch() {
  const [active, setActive] = useState(false);

  const triggerGlitch = useCallback(() => {
    setActive(true);
  }, []);

  useEffect(() => {
    if (!active) return;
    const el = document.body;
    el.classList.add("screen-glitch");
    const timeout = setTimeout(() => {
      el.classList.remove("screen-glitch");
      setActive(false);
    }, GLITCH_DURATION);
    return () => {
      clearTimeout(timeout);
      el.classList.remove("screen-glitch");
    };
  }, [active]);

  const GlitchOverlay = useMemo(
    () => () => <GlitchOverlayEl active={active} />,
    [active],
  );

  return useMemo(
    () => ({ triggerGlitch, GlitchOverlay, isGlitching: active }),
    [triggerGlitch, GlitchOverlay, active],
  );
}
