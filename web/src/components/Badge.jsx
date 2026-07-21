const variants = {
  default: "bg-[var(--surface-2)] text-[var(--muted)]",
  success: "bg-green-500/10 text-[var(--success)]",
  warning: "bg-amber-500/10 text-[var(--warning)]",
  danger: "bg-red-500/10 text-[var(--danger)]",
};

export function Badge({ className = "", variant = "default", ...props }) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium ${variants[variant]} ${className}`}
      {...props}
    />
  );
}
