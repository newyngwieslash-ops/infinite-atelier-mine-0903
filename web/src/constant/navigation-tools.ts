import { BookOpen, Clapperboard, Images, ListChecks, Maximize2, Settings2 } from "lucide-react";

export const navigationTools = [
    {
        // The studio is the drama side and the canvas is the free side, so the
        // studio leads: a project made here is the one the other tools serve.
        slug: "studio",
        icon: BookOpen,
    },
    {
        slug: "canvas",
        icon: Maximize2,
    },
    {
        slug: "director",
        icon: Clapperboard,
    },
    {
        slug: "assets",
        icon: Images,
    },
    {
        slug: "jobs",
        icon: ListChecks,
    },
    {
        slug: "config",
        icon: Settings2,
    },
] as const;

export type NavigationToolSlug = (typeof navigationTools)[number]["slug"];
