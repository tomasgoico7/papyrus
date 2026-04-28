import { forwardRef } from "react";

import { cn } from "@/lib/utils";

type Variant = "primary" | "secondary" | "ghost" | "destructive";
type Size = "sm" | "md" | "lg";

const VARIANTS: Record<Variant, string> = {
  primary:
    "bg-accent text-white shadow-subtle hover:brightness-[1.07] active:brightness-95",
  secondary: "border border-line bg-surface text-ink hover:bg-canvas",
  ghost: "text-ink-muted hover:text-ink",
  destructive:
    "bg-danger text-white shadow-subtle hover:brightness-[1.07] active:brightness-95",
};

const SIZES: Record<Size, string> = {
  sm: "h-9 px-4 text-sm",
  md: "h-11 px-5 text-[0.95rem]",
  lg: "h-14 px-8 text-base",
};

export function buttonClasses(variant: Variant = "primary", size: Size = "md") {
  return cn(
    "inline-flex select-none items-center justify-center gap-2 rounded-full font-medium tracking-tight transition-[filter,background-color,color,transform] duration-200 focus-visible:outline-none active:scale-[0.98] disabled:pointer-events-none disabled:opacity-40",
    VARIANTS[variant],
    SIZES[size],
  );
}

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ variant = "primary", size = "md", className, ...props }, ref) => (
    <button
      ref={ref}
      className={cn(buttonClasses(variant, size), className)}
      {...props}
    />
  ),
);

Button.displayName = "Button";
