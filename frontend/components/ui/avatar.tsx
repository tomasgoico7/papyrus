import type { AuthenticatedUser } from "@/lib/types";
import { cn } from "@/lib/utils";

export function Avatar({
  user,
  className,
}: {
  user: AuthenticatedUser;
  className?: string;
}) {
  if (user.avatarUrl) {
    return (
      <img
        src={user.avatarUrl}
        alt=""
        // Google serves avatars with a 403 when a cross-site referrer is sent.
        referrerPolicy="no-referrer"
        className={cn("h-8 w-8 rounded-full object-cover", className)}
      />
    );
  }

  const initial = (user.fullName ?? user.email).charAt(0).toUpperCase();
  return (
    <span
      className={cn(
        "grid h-8 w-8 place-items-center rounded-full bg-ink text-sm font-medium text-canvas",
        className,
      )}
    >
      {initial}
    </span>
  );
}
