"use client";

export function ConfirmSubmitButton({
  children,
  message,
  className,
  disabled = false,
  title,
}: {
  children: React.ReactNode;
  message: string;
  className?: string;
  disabled?: boolean;
  title?: string;
}) {
  return (
    <button
      type="submit"
      className={className}
      disabled={disabled}
      title={title}
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
