// Checkbox and Switch wrap a real (visually hidden) input, so keyboards,
// screen readers and labels work as usual.

interface CheckboxProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  label: string;
  className?: string;
}

export function Checkbox({ checked, onChange, disabled, label, className = "" }: CheckboxProps) {
  return (
    <span className={`relative inline-flex shrink-0 ${className}`}>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        aria-label={label}
        className="peer absolute inset-0 z-10 size-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
      />
      <span
        aria-hidden="true"
        className="grid size-[22px] place-items-center rounded-full border-[1.5px] border-faint transition peer-checked:border-accent peer-checked:bg-accent peer-focus-visible:ring-2 peer-focus-visible:ring-accent/40 peer-disabled:opacity-40 [&>svg]:opacity-0 peer-checked:[&>svg]:opacity-100"
      >
        <svg viewBox="0 0 16 16" className="size-3 text-white" fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round">
          <path d="M3.5 8.5l3 3 6-7" />
        </svg>
      </span>
    </span>
  );
}

export function Switch({ checked, onChange, disabled, label }: Omit<CheckboxProps, "className">) {
  return (
    <span className="relative inline-flex shrink-0">
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        aria-label={label}
        className="peer absolute inset-0 z-10 size-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
      />
      <span aria-hidden="true" className="h-[30px] w-[50px] rounded-full bg-fill ring-1 ring-line ring-inset transition-colors duration-200 peer-checked:bg-good peer-checked:ring-transparent peer-focus-visible:ring-2 peer-focus-visible:ring-accent/40 peer-disabled:opacity-40" />
      <span aria-hidden="true" className="pointer-events-none absolute top-[2px] left-[2px] size-[26px] rounded-full bg-white shadow-[0_2px_6px_rgb(0_0_0/0.2)] transition-transform duration-300 ease-pop peer-checked:translate-x-5 peer-active:scale-x-110" />
    </span>
  );
}
