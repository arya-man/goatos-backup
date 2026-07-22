"use client";

import { useRef, type KeyboardEvent, type MouseEvent } from "react";

type NativeDateInputElement = HTMLInputElement & {
  showPicker?: () => void;
};

export function NativeDateInput({
  name,
  label,
  min,
  required,
}: {
  name: string;
  label: string;
  min?: string;
  required?: boolean;
}) {
  const inputRef = useRef<NativeDateInputElement>(null);

  function openPicker(): void {
    const input = inputRef.current;
    if (!input) return;
    input.focus();
    try {
      input.showPicker?.();
    } catch {
      // Browser native date inputs still work when showPicker is unavailable.
    }
  }

  function handleClick(event: MouseEvent<HTMLInputElement>): void {
    event.currentTarget.focus();
    openPicker();
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>): void {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    openPicker();
  }

  return (
    <input
      ref={inputRef}
      name={name}
      aria-label={label}
      type="date"
      min={min}
      required={required}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
    />
  );
}
