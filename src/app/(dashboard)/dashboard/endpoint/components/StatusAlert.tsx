"use client";

import { CheckCircle2, CircleAlert, Info, TriangleAlert } from "lucide-react";

export interface StatusAlertData {
  type: "success" | "warning" | "info" | "error" | string;
  message: string;
}

export interface StatusAlertProps {
  status: StatusAlertData;
  className?: string;
}

/** Reusable status alert */
export default function StatusAlert({ status, className = "" }: StatusAlertProps) {
  const renderMessage = (msg: string) => {
    const parts = msg.split(/(https?:\/\/[^\s]+)/g);
    return parts.map((part, i) =>
      /^https?:\/\//.test(part) ? (
        <a
          key={i}
          href={part}
          target="_blank"
          rel="noreferrer"
          className="underline font-medium"
        >
          {part}
        </a>
      ) : (
        part
      ),
    );
  };

  const Icon =
    status.type === "success"
      ? CheckCircle2
      : status.type === "warning"
        ? TriangleAlert
        : status.type === "info"
          ? Info
          : CircleAlert;

  return (
    <div
      role="status"
      aria-live="polite"
      className={`flex items-start gap-2 rounded-[5px] border border-current/15 p-2 text-sm ${className} ${
        status.type === "success"
          ? "bg-green-500/10 text-green-600 dark:text-green-400"
          : status.type === "warning"
            ? "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400"
            : status.type === "info"
              ? "bg-blue-500/10 text-blue-600 dark:text-blue-400"
              : "bg-red-500/10 text-red-600 dark:text-red-400"
      }`}
    >
      <Icon
        size={18}
        strokeWidth={1.75}
        aria-hidden="true"
        className="mt-0.5 shrink-0"
      />
      <span>{renderMessage(status.message)}</span>
    </div>
  );
}
