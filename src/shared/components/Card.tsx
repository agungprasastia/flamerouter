"use client";

import React, { ReactNode, HTMLAttributes } from "react";
import { cn } from "@/shared/utils/cn";

export interface CardProps extends HTMLAttributes<HTMLDivElement> {
  children?: ReactNode;
  title?: ReactNode | string;
  subtitle?: ReactNode | string;
  icon?: string | ReactNode;
  action?: ReactNode;
  padding?: "none" | "xs" | "sm" | "md" | "lg" | string;
  hover?: boolean;
  elev?: boolean;
  className?: string;
}

export default function Card({
  children,
  title,
  subtitle,
  icon,
  action,
  padding = "md",
  hover = false,
  elev = false,
  className,
  ...props
}: CardProps) {
  const paddings: Record<string, string> = {
    none: "",
    xs: "p-3",
    sm: "p-4",
    md: "p-6",
    lg: "p-8",
  };

  return (
    <div
      className={cn(
        "bg-surface border border-border-subtle rounded-[10px_2px_10px_2px]",
        elev && "shadow-[var(--shadow-elev)]",
        hover &&
          "hover:-translate-y-0.5 hover:border-brand-500/40 transition-[transform,border-color] cursor-pointer",
        paddings[padding] || paddings.md,
        className,
      )}
      {...props}
    >
      {(title || action) && (
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            {icon && (
              <div className="p-2 rounded-[6px_1px_6px_1px] bg-bg text-primary">
                {typeof icon === "string" ? (
                  <span className="material-symbols-outlined text-[20px]">
                    {icon}
                  </span>
                ) : (
                  icon
                )}
              </div>
            )}
            <div>
              {title && (
                <h3 className="text-text-main font-semibold">{title}</h3>
              )}
              {subtitle && (
                <p className="text-sm text-text-muted">{subtitle}</p>
              )}
            </div>
          </div>
          {action}
        </div>
      )}
      {children}
    </div>
  );
}

Card.Section = function CardSection({ children, className, ...props }: any) {
  return (
    <div
      className={cn(
        "p-4 rounded-[6px_1px_6px_1px]",
        "bg-bg-alt/55 border border-border-subtle",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
};

Card.Row = function CardRow({ children, className, ...props }: any) {
  return (
    <div
      className={cn(
        "p-3 -mx-3 px-3 transition-colors",
        "border-b border-border-subtle last:border-b-0",
        "hover:bg-surface-2/50",
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
};

Card.ListItem = function CardListItem({
  children,
  actions,
  className,
  ...props
}: any) {
  return (
    <div
      className={cn(
        "group flex items-center justify-between p-3 -mx-3 px-3",
        "border-b border-border-subtle last:border-b-0",
        "hover:bg-surface-2/50 transition-colors",
        className,
      )}
      {...props}
    >
      <div className="flex-1 min-w-0">{children}</div>
      {actions && (
        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
          {actions}
        </div>
      )}
    </div>
  );
};
