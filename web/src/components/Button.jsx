const variants = {
  primary: "border-transparent bg-[var(--primary)] text-[var(--primary-contrast)] hover:brightness-105",
  secondary: "border-[var(--border)] bg-[var(--surface-2)] text-[var(--text)] hover:border-[var(--primary)]/50",
  ghost: "border-transparent text-[var(--muted)] hover:bg-[var(--surface-2)] hover:text-[var(--text)]",
  danger: "border-transparent bg-[var(--danger)] text-white hover:brightness-105",
};

const sizes = {
  sm: "h-8 px-3 text-xs",
  md: "h-10 px-4 text-sm",
};

export function Button({ className = "", variant = "primary", size = "md", ...props }) {
  return (
    <button
      className={`inline-flex items-center justify-center gap-2 rounded-[10px] border font-medium transition active:translate-y-px disabled:cursor-not-allowed disabled:opacity-50 focus-visible:focus-ring ${variants[variant]} ${sizes[size]} ${className}`}
      {...props}
    />
  );
}
