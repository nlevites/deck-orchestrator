import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
// Opt in to React Router v7 transition and splat-path semantics so the
// console shell doesn't print future-flag warnings to the dev console.
const ROUTER_FUTURE = {
  v7_startTransition: true,
  v7_relativeSplatPath: true,
} as const;
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "@/App";
import { ToastProvider } from "@/lib/toasts/ToastContext";
import "@/styles/globals.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // The console's read path is the live polling hook (see
      // lib/live/use-live-state.ts), not refetch-on-focus. Pages mark
      // their queries as cache-only (staleTime: Infinity, throwing
      // queryFn) so these defaults are mostly inert; we leave the
      // window-focus refetch off so a tab switch doesn't trigger a
      // throw on the cache-only queryFn.
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      retry: 1,
    },
  },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter future={ROUTER_FUTURE}>
        <ToastProvider>
          <App />
        </ToastProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
);
