"use client";

import * as React from "react";
import { motion, useAnimationFrame } from "motion/react";
import { cn } from "@/lib/utils";

export interface AuroraBarsProps {
  barCount?: number; // default: 24
  colors?: string[]; // default: --aurora-1..4 (theme-aware), then transparent
  maxHeightRatio?: number; // default: 0.92
  minHeightRatio?: number; // default: 0.18
  speed?: number; // default: 0.5
  gap?: number; // default: 3
  blur?: number; // default: 0
  background?: string; // default: var(--aurora-bg)
  className?: string;
}

function barHeight(
  index: number,
  total: number,
  time: number,
  minH: number,
  maxH: number,
): number {
  const norm = index / (total - 1);
  const arch = Math.sin(norm * Math.PI);
  const phase1 = (index / total) * Math.PI * 2;
  const phase2 = (index / total) * Math.PI * 5.3;
  const wave = 0.5 + 0.25 * Math.sin(time * 1.1 + phase1) + 0.25 * Math.sin(time * 0.7 + phase2);
  const blended = arch * 0.65 + wave * 0.35;
  return minH + blended * (maxH - minH);
}

export function AuroraBars({
  barCount = 24,
  colors = ["var(--aurora-1)", "var(--aurora-2)", "var(--aurora-3)", "var(--aurora-4)", "transparent"],
  maxHeightRatio = 0.92,
  minHeightRatio = 0.18,
  speed = 0.5,
  gap = 3,
  blur = 0,
  background = "var(--aurora-bg)",
  className,
}: AuroraBarsProps) {
  const containerRef = React.useRef<HTMLDivElement>(null);
  const [heights, setHeights] = React.useState<number[]>(() =>
    Array.from({ length: barCount }, (_, i) =>
      barHeight(i, barCount, 0, minHeightRatio, maxHeightRatio),
    ),
  );
  const timeRef = React.useRef(0);

  useAnimationFrame((_, delta) => {
    timeRef.current += (delta / 1000) * speed;
    const t = timeRef.current;
    setHeights(
      Array.from({ length: barCount }, (_, i) =>
        barHeight(i, barCount, t, minHeightRatio, maxHeightRatio),
      ),
    );
  });

  const gradientStop = colors
    .map((c, i) => `${c} ${Math.round((i / (colors.length - 1)) * 100)}%`)
    .join(", ");
  const gradient = `linear-gradient(to top, ${gradientStop})`;

  return (
    <div
      ref={containerRef}
      className={cn("relative w-full h-full overflow-hidden", className)}
      style={{ background }}
    >
      <div className="absolute inset-0 flex items-end" style={{ filter: `blur(${blur}px)` }}>
        {Array.from({ length: barCount }).map((_, i) => {
          const heightFraction = heights[i] ?? maxHeightRatio;
          return (
            <div
              key={i}
              className="flex-1"
              style={{
                height: "100%",
                display: "flex",
                alignItems: "flex-end",
                padding: `0 ${gap / 2}px`,
              }}
            >
              <motion.div
                style={{
                  width: "100%",
                  height: `${heightFraction * 100}%`,
                  background: gradient,
                  borderRadius: "9999px 9999px 0 0",
                  opacity: 0.85,
                }}
              />
            </div>
          );
        })}
      </div>
      <div
        className="absolute inset-0 pointer-events-none"
        style={{
          background: "radial-gradient(ellipse 90% 80% at 50% 100%, transparent 40%, var(--aurora-vignette) 100%)",
        }}
      />
    </div>
  );
}
