// Types for the part of foliate-js's view.js that Streamero uses (the module is plain JavaScript).
export function makeBook(file: File | Blob | string): Promise<{ dir?: string; transformTarget?: EventTarget }>;
