export function Icon({ name }: { name: string }) {
  const paths: Record<string, string> = {
    Stacks: "m12 3 9 5-9 5-9-5 9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5",
    Repository:
      "M6 7v10m12-10v3a4 4 0 0 1-4 4H6M6 3a2 2 0 1 0 0 4 2 2 0 0 0 0-4Zm0 14a2 2 0 1 0 0 4 2 2 0 0 0 0-4ZM18 3a2 2 0 1 0 0 4 2 2 0 0 0 0-4Z",
    Operations: "m4 5 6 7-6 7m9 0h7",
    Audit: "M6 3h8l4 4v14H6V3Zm8 0v5h4M9 12h6m-6 4h6",
    Settings:
      "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8Zm-2-5h4l1 3 3 1 3 3v4l-3 1-1 3-3 3h-4l-1-3-3-1-3-3v-4l3-1 1-3 3-3Z",
    Plus: "M12 5v14M5 12h14",
    Close: "m6 6 12 12M6 18 18 6",
    Search: "M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14Zm5 12 6 6",
    Refresh: "M20 7a9 9 0 1 0 1 8M20 2v6h-6",
    Pull: "M12 3v13m-5-5 5 5 5-5M4 21h16",
    Push: "M12 17V4m-5 5 5-5 5 5M4 21h16",
    File: "M5 2h9l5 5v15H5V2Zm9 0v6h5M8 12h8m-8 4h8",
    Folder: "M3 6V4h6l3 3h9v13H3V6Z",
    Check: "m4 12 5 5L20 6",
    Stop: "M5 5h14v14H5Z",
    Play: "m7 3 14 9-14 9V3Z",
    Chevron: "m9 5 7 7-7 7",
  };
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.6"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name] || paths.File} />
    </svg>
  );
}
