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
        queryParams: { prompt: "select_account" },
      },
    });

    if (error) {
      setPending(false);
      console.error("Google sign-in failed:", error.message);
    }
  }

  return (
    <Button onClick={signIn} disabled={pending} variant={variant} size={size}>
      {pending && <Spinner />}
      {label}
    </Button>
  );
}
