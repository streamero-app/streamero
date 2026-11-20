import "@testing-library/jest-dom/vitest";
import { configure } from "@testing-library/react";
import "./i18n";

// Pages load their chunks lazily; on a busy machine the first render of a page can take
// longer than the default 1 s of findBy*/waitFor, which made tests fail at random.
configure({ asyncUtilTimeout: 4000 });
