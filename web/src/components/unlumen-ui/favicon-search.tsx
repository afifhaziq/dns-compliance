"use client";
// ui.unlumen.com/components/favicon-search

import { AnimatePresence, motion } from "motion/react";
import { Globe, Search, X } from "lucide-react";
import { useEffect, useState } from "react";
import { normalizeForClient } from "@/routes/__root";
import { cn } from "@/lib/utils";

export interface FaviconSearchProps {
  value?: string;
  defaultValue?: string;
  onChange?: (value: string) => void;
  onSearch?: (value: string, domain: string) => void;
  placeholder?: string;
  clearable?: boolean;
  faviconSize?: 16 | 32 | 64 | 128;
  debounce?: number;
  className?: string;
  inputClassName?: string;
}

export function faviconUrl(domain: string, size: number) {
  return `https://www.google.com/s2/favicons?domain=${encodeURIComponent(domain)}&sz=${size}`;
}

export function FaviconSearch({
  value: valueProp,
  defaultValue = "",
  onChange,
  onSearch,
  placeholder = "Enter a website URL…",
  clearable = true,
  faviconSize = 64,
  debounce = 350,
  className,
  inputClassName,
}: FaviconSearchProps) {
  const [internal, setInternal] = useState(defaultValue);
  const controlled = valueProp !== undefined;
  const value = controlled ? valueProp! : internal;

  const [resolvedDomain, setResolvedDomain] = useState("");
  const [status, setStatus] = useState<"idle" | "loading" | "loaded" | "error">("idle");

  const setValue = (v: string) => {
    if (!controlled) setInternal(v);
    onChange?.(v);
  };

  // Debounced domain resolution + favicon preload, so a load/error state is
  // known before the icon animates in (avoids a flash of a broken image).
  useEffect(() => {
    const host = normalizeForClient(value.trim())
    if (!host || !host.includes(".")) {
      setResolvedDomain("");
      setStatus("idle");
      return;
    }
    setStatus("loading");
    const handle = setTimeout(() => {
      const img = new Image();
      img.onload = () => { setResolvedDomain(host); setStatus("loaded"); };
      img.onerror = () => { setResolvedDomain(""); setStatus("error"); };
      img.src = faviconUrl(host, faviconSize);
    }, debounce);
    return () => clearTimeout(handle);
  }, [value, debounce, faviconSize]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const host = normalizeForClient(value.trim());
    if (host) onSearch?.(value.trim(), host);
  };

  return (
    <form onSubmit={handleSubmit} className={cn("relative flex items-center", className)}>
      <span className="absolute left-3 flex items-center justify-center w-4 h-4 pointer-events-none text-stone-muted">
        <AnimatePresence mode="wait" initial={false}>
          {status === "loaded" && resolvedDomain ? (
            <motion.img
              key={resolvedDomain}
              src={faviconUrl(resolvedDomain, faviconSize)}
              alt=""
              width={16}
              height={16}
              initial={{ opacity: 0, scale: 0.6 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.6 }}
              transition={{ type: "spring", stiffness: 300, damping: 22 }}
            />
          ) : status === "loading" ? (
            <motion.span key="loading" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
              <Globe className="w-4 h-4 animate-pulse" />
            </motion.span>
          ) : (
            <motion.span key="fallback" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
              <Search className="w-4 h-4" />
            </motion.span>
          )}
        </AnimatePresence>
      </span>

      <input
        type="text"
        value={value}
        onChange={e => setValue(e.target.value)}
        placeholder={placeholder}
        className={cn("form-input", inputClassName)}
        // form-input's px-3 comes from an `@apply` inside the class itself, so
        // tailwind-merge can't see and override it via a pl-*/pr-* class — an
        // inline style is the one thing guaranteed to win the cascade here.
        style={{ paddingLeft: 36, paddingRight: clearable && value ? 32 : undefined }}
        aria-label={placeholder}
      />

      {clearable && value && (
        <button
          type="button"
          className="absolute right-2 flex items-center justify-center w-5 h-5 text-stone-muted hover:text-foreground"
          onClick={() => setValue("")}
          aria-label="Clear"
        >
          <X className="w-3.5 h-3.5" />
        </button>
      )}
    </form>
  );
}
