import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { i18nReady } from "./i18n";
import "./styles.css";
import { router } from "./app/router";
import { createQueryClient } from "./app/queryClient";

const queryClient = createQueryClient();

// The language chunk was requested while this bundle was still being parsed;
// rendering waits for it so no screen ever flashes untranslated keys.
void i18nReady.finally(() => {
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>,
  );
});
