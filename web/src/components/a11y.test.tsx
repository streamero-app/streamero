import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useRef, useState } from "react";
import { afterEach, vi } from "vitest";
import { setLanguage } from "@/i18n";
import { mockApi, renderApp, testUser } from "@/test-utils";
import { useFocusTrap } from "./focusTrap";
import { useMenu } from "./menu";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => setLanguage("en"));

function MenuHarness() {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);
  const onKey = useMenu(open, setOpen, root, button);
  return (
    <div ref={root}>
      <button ref={button} type="button" aria-haspopup="menu" onClick={() => setOpen(!open)}>open</button>
      {open && (
        <div role="menu" onKeyDown={onKey}>
          <button type="button" role="menuitem">One</button>
          <button type="button" role="menuitem">Two</button>
          <button type="button" role="menuitem">Three</button>
        </div>
      )}
      <button type="button">outside</button>
    </div>
  );
}

describe("menus", () => {
  it("moves focus in, along with the arrows, and back to the button on Escape", () => {
    render(<MenuHarness />);
    fireEvent.click(screen.getByText("open"));
    expect(screen.getByRole("menuitem", { name: "One" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
    expect(screen.getByRole("menuitem", { name: "Two" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "End" });
    expect(screen.getByRole("menuitem", { name: "Three" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
    expect(screen.getByRole("menuitem", { name: "One" })).toHaveFocus();
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByText("open")).toHaveFocus();
  });

  it("the account menu opens with focus on its first item", async () => {
    mockApi(() => ({ status: 200, body: [] }));
    renderApp("/", { me: testUser() });
    fireEvent.click(await screen.findByTestId("account-menu"));
    await waitFor(() => expect(screen.getAllByRole("menuitem")[0]).toHaveFocus());
  });
});

function TrapHarness({ onClose }: { onClose: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref, "[data-close]");
  return (
    <div ref={ref} role="dialog" aria-modal>
      <button type="button" data-close onClick={onClose}>close</button>
      <button type="button">next</button>
    </div>
  );
}

describe("full-screen dialogs", () => {
  it("take focus, keep Tab inside and give focus back", () => {
    function Page() {
      const [open, setOpen] = useState(false);
      return <><button type="button" onClick={() => setOpen(true)}>show</button>{open && <TrapHarness onClose={() => setOpen(false)} />}</>;
    }
    render(<Page />);
    const show = screen.getByText("show");
    show.focus();
    fireEvent.click(show);
    expect(screen.getByText("close")).toHaveFocus();
    screen.getByText("next").focus();
    fireEvent.keyDown(document, { key: "Tab" });
    expect(screen.getByText("close")).toHaveFocus();
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(screen.getByText("next")).toHaveFocus();
    fireEvent.click(screen.getByText("close"));
    expect(show).toHaveFocus();
  });
});

describe("skip link", () => {
  it("leads past the header to the page", async () => {
    mockApi(() => ({ status: 200, body: [] }));
    renderApp("/", { me: testUser() });
    const skip = await screen.findByRole("link", { name: "Skip to content" });
    expect(skip).toHaveAttribute("href", "#main");
    expect(document.getElementById("main")).toBeInTheDocument();
  });
});
