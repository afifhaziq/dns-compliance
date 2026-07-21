"use client";

import { motion, useSpring, useTransform } from "motion/react";
import { useEffect } from "react";

interface AnimatedNumberProps {
  value: number;
  className?: string;
}

/** Rolls smoothly to `value` on change instead of snapping. */
export function AnimatedNumber({ value, className }: AnimatedNumberProps) {
  const spring = useSpring(value, { stiffness: 300, damping: 30 });
  const display = useTransform(spring, (v) => Math.max(0, Math.round(v)).toLocaleString());

  useEffect(() => {
    spring.set(value);
  }, [value, spring]);

  return <motion.span className={className}>{display}</motion.span>;
}

export default AnimatedNumber;
