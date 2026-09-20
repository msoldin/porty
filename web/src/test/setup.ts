import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/preact";
import { afterEach } from "vitest";
afterEach(cleanup);
// jsdom has no layout; CodeMirror measures DOM ranges during animation frames.
Range.prototype.getClientRects = () => [] as unknown as DOMRectList;
Range.prototype.getBoundingClientRect = () => new DOMRect();
