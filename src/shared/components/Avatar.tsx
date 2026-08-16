"use client";

import React from "react";
import { cn } from "@/shared/utils/cn";

export interface AvatarProps {
  src?: string | null;
  alt?: string;
  name?: string;
  size?: "xs" | "sm" | "md" | "lg" | "xl" | string;
  className?: string;
  providerId?: string;
  fallbackText?: string;
  fallbackColor?: string;
}

export default function Avatar({
  src,
  alt = "Avatar",
  name,
  size = "md",
  className,
  providerId,
  fallbackText,
  fallbackColor,
}: AvatarProps) {
  const sizes: Record<string, string> = {
    xs: "size-6 text-xs",
    sm: "size-8 text-sm",
    md: "size-10 text-base",
    lg: "size-12 text-lg",
    xl: "size-16 text-xl",
  };

  // Get initials from name
  const getInitials = (name?: string) => {
    if (fallbackText) return fallbackText;
    if (!name) return "?";
    const parts = name.split(" ");
    if (parts.length >= 2) {
      return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
    }
    return name.substring(0, 2).toUpperCase();
  };

  // Generate color from name
  const getColorFromName = (name?: string) => {
    if (fallbackColor) return fallbackColor;
    if (!name) return "bg-primary";
    const colors = [
      "bg-red-500",
      "bg-orange-500",
      "bg-amber-500",
      "bg-yellow-500",
      "bg-lime-500",
      "bg-green-500",
      "bg-emerald-500",
      "bg-teal-500",
      "bg-cyan-500",
      "bg-sky-500",
      "bg-blue-500",
      "bg-indigo-500",
      "bg-violet-500",
      "bg-purple-500",
      "bg-fuchsia-500",
      "bg-pink-500",
      "bg-rose-500",
    ];
    const index = name.charCodeAt(0) % colors.length;
    return colors[index];
  };

  if (src) {
    return (
      <div
        className={cn(
          "rounded-full bg-cover bg-center bg-no-repeat",
          "ring-2 ring-white dark:ring-surface-dark shadow-sm",
          sizes[size] || sizes.md,
          className,
        )}
        style={{ backgroundImage: `url(${src})` }}
        role="img"
        aria-label={alt}
      />
    );
  }

  return (
    <div
      className={cn(
        "rounded-full flex items-center justify-center font-semibold text-white",
        "ring-2 ring-white dark:ring-surface-dark shadow-sm",
        sizes[size] || sizes.md,
        getColorFromName(name),
        className,
      )}
      role="img"
      aria-label={alt}
    >
      {getInitials(name)}
    </div>
  );
}
