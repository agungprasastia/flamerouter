export function Input({ id, label, helper, error, className = "", ...props }) {
  return (
    <label className="block space-y-1.5 text-sm text-[var(--text)]" htmlFor={id}>
      {label && <span className="font-medium">{label}</span>}
      <input
        id={id}
        className={`h-10 w-full rounded-[10px] border border-[var(--border)] bg-[var(--surface-2)] px-3 text-sm text-[var(--text)] outline-none transition placeholder:text-[var(--muted)]/70 focus:border-[var(--primary)] ${className}`}
        {...props}
      />
      {helper && !error && <span className="block text-xs text-[var(--muted)]">{helper}</span>}
      {error && <span className="block text-xs text-[var(--danger)]">{error}</span>}
    </label>
  );
}
