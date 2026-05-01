"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { createClient } from "@/lib/supabase/client";

interface SignInButtonProps {
  label?: string;
  variant?: "primary" | "secondary";
  size?: "sm" | "md" | "lg";
}

export function SignInButton({
  label = "Continue with Google",
  variant = "primary",
  size = "md",
}: SignInButtonProps) {
  const [pending, setPending] = useState(false);

  async function signIn() {
    setPending(true);
    const supabase = createClient();
    const { error } = await supabase.auth.signInWithOAuth({
      provider: "google",
      options: {
        redirectTo: `${window.location.origin}/auth/callback?next=/dashboard`,
      },
    });

    // On success the browser is already navigating away; only a failed
    // hand-off returns here, so we surface it and re-enable the control.
    if (error) {
      setPending(false);
      console.error("Google sign-in failed:", error.message);
    }
  }

  return (
    <Button onClick={signIn} disabled={pending} variant={variant} size={size}>
      {pending ? <Spinner /> : <GoogleMark />}
      {label}
    </Button>
  );
}

function GoogleMark() {
  return (
    <svg viewBox="0 0 18 18" className="h-[18px] w-[18px]" aria-hidden>
      <path
        fill="#FFC107"
        d="M17.6 9.2c0-.6-.05-1.18-.15-1.74H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.64-3.88 2.64-6.72Z"
      />
      <path
        fill="#FF3D00"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.03-3.7H.95v2.33A9 9 0 0 0 9 18Z"
      />
      <path
        fill="#4CAF50"
        d="M3.97 10.72A5.4 5.4 0 0 1 3.68 9c0-.6.1-1.18.29-1.72V4.95H.95A9 9 0 0 0 0 9c0 1.45.35 2.82.95 4.05l3.02-2.33Z"
      />
      <path
        fill="#1976D2"
        d="M9 3.58c1.32 0 2.5.46 3.44 1.35l2.58-2.58A8.98 8.98 0 0 0 .95 4.95l3.02 2.33C4.68 5.16 6.66 3.58 9 3.58Z"
      />
    </svg>
  );
}
