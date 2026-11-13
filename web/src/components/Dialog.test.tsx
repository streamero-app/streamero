import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { setLanguage } from "@/i18n";
import { DialogProvider, useConfirm, usePrompt } from "./Dialog";

function Harness({ onResult }: { onResult: (v: unknown) => void }) {
  const confirm = useConfirm();
  const prompt = usePrompt();
  return (
    <>
      <button type="button" onClick={() => void confirm({ message: "Delete it?", danger: true }).then(onResult)}>ask</button>
      <button type="button" onClick={() => void prompt({ message: "Name" }).then(onResult)}>name</button>
    </>
  );
}

const setup = () => {
  setLanguage("en");
  const results: unknown[] = [];
  render(<DialogProvider><Harness onResult={(v) => results.push(v)} /></DialogProvider>);
  return results;
};

describe("themed dialogs", () => {
  it("confirms, and focuses the safe choice first", async () => {
    const results = setup();
    screen.getByText("ask").focus();
    fireEvent.click(screen.getByText("ask"));
    const dialog = await screen.findByRole("alertdialog", { name: "Delete it?" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await waitFor(() => expect(results).toEqual([true]));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(screen.getByText("ask")).toHaveFocus(); // focus goes back
  });

  it("cancels with Escape and keeps Tab inside", async () => {
    const results = setup();
    fireEvent.click(screen.getByText("ask"));
    const dialog = await screen.findByRole("alertdialog");
    const confirm = screen.getByRole("button", { name: "Confirm" });
    confirm.focus();
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(screen.getByRole("button", { name: "Cancel" })).toHaveFocus();
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(confirm).toHaveFocus();
    fireEvent.keyDown(dialog, { key: "Escape" });
    await waitFor(() => expect(results).toEqual([false]));
  });

  it("prompts for text and returns it trimmed (null when cancelled)", async () => {
    const results = setup();
    fireEvent.click(screen.getByText("name"));
    const field = await screen.findByRole("textbox", { name: "Name" });
    expect(field).toHaveFocus();
    expect(screen.getByRole("button", { name: "OK" })).toBeDisabled();
    fireEvent.change(field, { target: { value: "  Friday films " } });
    fireEvent.click(screen.getByRole("button", { name: "OK" }));
    await waitFor(() => expect(results).toEqual(["Friday films"]));
    fireEvent.click(screen.getByText("name"));
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(results).toEqual(["Friday films", null]));
  });
});
