"use client";

import { Suspense } from "react";
import { useOAuthCallback } from "@/shared/hooks";

/**
 * OAuth Callback Page Content
 */
function CallbackContent() {
  const { status } = useOAuthCallback();

  return (
    <div className="min-h-screen flex items-center justify-center bg-bg">
      <div className="text-center p-8 max-w-md">
        {status === "processing" && (
          <>
            <div className="size-16 mx-auto mb-4 rounded-full bg-primary/10 flex items-center justify-center">
              <span className="material-symbols-outlined text-3xl text-primary animate-spin">
                progress_activity
              </span>
            </div>
            <h1 className="text-xl font-semibold mb-2">Processing...</h1>
            <p className="text-text-muted">
              Please wait while we complete the authorization.
            </p>
          </>
        )}

        {(status === "success" || status === "done") && (
          <>
            <div className="size-16 mx-auto mb-4 rounded-full bg-green-100 dark:bg-green-900/30 flex items-center justify-center">
              <span className="material-symbols-outlined text-3xl text-green-600">
                check_circle
              </span>
            </div>
            <h1 className="text-xl font-semibold mb-2">
              Authorization Successful!
            </h1>
            <p className="text-text-muted">
              {status === "success"
                ? "This window will close automatically..."
                : "You can close this tab now."}
            </p>
          </>
        )}

        {status === "manual" && (
          <>
            <div className="size-16 mx-auto mb-4 rounded-full bg-yellow-100 dark:bg-yellow-900/30 flex items-center justify-center">
              <span className="material-symbols-outlined text-3xl text-yellow-600">
                info
              </span>
            </div>
            <h1 className="text-xl font-semibold mb-2">Copy This URL</h1>
            <p className="text-text-muted mb-4">
              Please copy the URL from the address bar and paste it in the
              application.
            </p>
            <div className="bg-surface border border-border rounded-lg p-3 text-left">
              <code className="text-xs break-all">
                {typeof window !== "undefined" ? window.location.href : ""}
              </code>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

/**
 * OAuth Callback Page
 * Receives callback from OAuth providers and sends data back via multiple methods
 */
export default function CallbackPage() {
  return (
    <Suspense
      fallback={
        <div className="min-h-screen flex items-center justify-center bg-bg">
          <div className="text-center p-8">
            <div className="size-16 mx-auto mb-4 rounded-full bg-primary/10 flex items-center justify-center">
              <span className="material-symbols-outlined text-3xl text-primary animate-spin">
                progress_activity
              </span>
            </div>
            <p className="text-text-muted">Loading...</p>
          </div>
        </div>
      }
    >
      <CallbackContent />
    </Suspense>
  );
}
