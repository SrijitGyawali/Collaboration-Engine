// TextareaBinding with two "tabs": fake textareas bound to sessions on an
// in-memory hub, driven like a user would (typing at the caret, Backspace).

import { beforeAll, describe, expect, it } from "vitest";

import type { CRDT } from "@/lib/crdt";
import { CollabSession } from "@/lib/sync/session";
import { FakeTextarea } from "@/test/fake-textarea";
import { Hub } from "@/test/hub";
import { loadTestCRDT } from "@/test/load-crdt";
import { TextareaBinding } from "./binding";

let crdt: CRDT;

beforeAll(async () => {
  crdt = await loadTestCRDT();
});

function tab(hub: Hub, name: string) {
  const el = new FakeTextarea();
  const session = new CollabSession(crdt, hub.connect(), name);
  const lengths: number[] = [];
  const binding = new TextareaBinding(el.asElement(), session, { onLengthChange: (n) => lengths.push(n) });
  return { el, session, binding, lengths };
}

describe("TextareaBinding", () => {
  it("syncs typing between two tabs", () => {
    const hub = new Hub();
    const a = tab(hub, "a");
    const b = tab(hub, "b");

    a.el.focus();
    a.el.type("hello");
    a.el.backspace();

    expect(a.el.value).toBe("hell");
    expect(b.el.value).toBe("hell");
    expect(b.lengths.at(-1)).toBe(4);
  });

  it("keeps the local caret next to the same text when a remote edit lands before it", () => {
    const hub = new Hub();
    const a = tab(hub, "a");
    const b = tab(hub, "b");
    a.el.focus();
    a.el.type("Hello world");
    a.el.setSelectionRange(5, 5); // caret after "Hello"

    b.el.focus();
    b.el.setSelectionRange(0, 0);
    b.el.type(">> ");
    a.el.focus(); // a is the tab whose caret we check

    expect(a.el.value).toBe(">> Hello world");
    expect(a.el.value.slice(0, a.el.selectionStart)).toBe(">> Hello");

    a.el.type("!");
    expect(b.el.value).toBe(">> Hello! world");
  });

  it("handles emoji, whose UTF-16 length is 2", () => {
    const hub = new Hub();
    const a = tab(hub, "a");
    const b = tab(hub, "b");
    a.el.focus();
    a.el.type("a👋b");
    a.el.setSelectionRange(3, 3); // after the emoji (UTF-16 index 3)

    b.el.focus();
    b.el.setSelectionRange(0, 0);
    b.el.type("👋");
    a.el.focus();

    expect(a.el.value).toBe("👋a👋b");
    expect(a.el.value.slice(0, a.el.selectionStart)).toBe("👋a👋");
    a.el.backspace();
    expect(b.el.value).toBe("👋ab");
    expect(a.lengths.at(-1)).toBe(3);
  });

  it("places a repeated character where the caret is", () => {
    const hub = new Hub();
    const a = tab(hub, "a");
    const b = tab(hub, "b");
    a.el.focus();
    a.el.type("aa");
    a.el.setSelectionRange(1, 1);
    a.el.type("a"); // "a|a" -> "aa|a": the new "a" is the middle one

    // b inserts "X" right after a's new character, by position.
    b.el.focus();
    b.el.setSelectionRange(2, 2);
    b.el.type("X");

    expect(a.el.value).toBe("aaXa");
    expect(b.el.value).toBe("aaXa");
  });

  it("does not move the selection of an unfocused textarea", () => {
    const hub = new Hub();
    const a = tab(hub, "a");
    const b = tab(hub, "b");
    b.el.focus();
    b.el.type("abc");

    expect(a.el.value).toBe("abc");
    expect(a.el.selectionStart).toBe(0); // untouched: a never had focus
  });

  it("stops syncing after dispose", () => {
    const hub = new Hub();
    const a = tab(hub, "a");
    const b = tab(hub, "b");
    a.binding.dispose();

    b.el.focus();
    b.el.type("x");
    a.el.type("y");

    expect(a.el.value).toBe("y");
    expect(b.el.value).toBe("x");
  });
});
