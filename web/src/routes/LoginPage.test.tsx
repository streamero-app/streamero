import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LoginPage } from "./LoginPage";
import { setLanguage } from "@/i18n";

function renderLogin() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(["system", "info"], {
    id: "x", name: "Test", product: "Streamero", version: "0.1.0",
    apiVersion: "v1", setupCompleted: false, startedAt: new Date().toISOString(),
  });
  return render(<QueryClientProvider client={qc}><LoginPage /></QueryClientProvider>);
}

describe("LoginPage", () => {
  it("renders in English and switches to Croatian", () => {
    setLanguage("en");
    renderLogin();
    expect(screen.getByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Language"), { target: { value: "hr" } });
    expect(screen.getByRole("heading", { name: "Prijava" })).toBeInTheDocument();
  });

  it("shows server version", () => {
    setLanguage("en");
    renderLogin();
    expect(screen.getByText(/Version 0\.1\.0/)).toBeInTheDocument();
  });
});
