"use client";

export function ConfirmSubmitButton({
  children,
  message,
  className,
  disabled = false,
}: {
  children: React.ReactNode;
  message: string;
  className?: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="submit"
      className={className}
      disabled={disabled}
      onClick={(event) => {
        if (disabled) {
          event.preventDefault();
          return;
        }
        if (!window.confirm(message)) {
          event.preventDefault();
        }
      }}
    >
      {children}
    </button>
  );
}
