import { cn } from "@/lib/utils";

export function Logo({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-2 text-[1.05rem] font-semibold tracking-tightest text-ink",
        className,
      )}
    >
      <span
        aria-hidden
        className="h-2.5 w-2.5 rounded-[3px] bg-accent"
      />
      Papyrus
    </span>
  );
}
