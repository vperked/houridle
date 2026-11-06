import { useCallback, useRef, useState } from "react";

export interface Pages<T> {
  items: T[]; // this page's items
  page: number; // from 0
  pages: number; // at least 1
  from: number; // index of the first item shown
  total: number;
  size: number;
  setPage: (page: number) => void;
  ref: (el: HTMLElement | null) => void; // for the list, brought back to its top on a page change
}

// usePages splits items into pages of size. It goes back to the first page
// whenever `reset` changes (a new search, filter or sort), and stays within
// the pages there are when the list shrinks.
export function usePages<T>(items: T[], size: number, reset: string): Pages<T> {
  const [at, setAt] = useState({ reset, page: 0 });
  const list = useRef<HTMLElement | null>(null);
  const ref = useCallback((el: HTMLElement | null) => {
    list.current = el;
  }, []);
  if (at.reset !== reset) setAt({ reset, page: 0 }); // during render, so the old page is never drawn
  const pages = Math.max(1, Math.ceil(items.length / size));
  const page = at.reset === reset ? Math.min(at.page, pages - 1) : 0;
  const from = page * size;
  const setPage = (p: number) => {
    setAt({ reset, page: Math.min(Math.max(0, p), pages - 1) });
    const el = list.current;
    if (!el) return;
    el.scrollTop = 0; // a list that scrolls on its own
    if (el.getBoundingClientRect().top < 0) el.scrollIntoView({ block: "start" }); // the page, when the list's top is off screen
  };
  return { items: items.slice(from, from + size), page, pages, from, total: items.length, size, setPage, ref };
}
