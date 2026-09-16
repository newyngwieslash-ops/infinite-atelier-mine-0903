import { Clapperboard, Images, ListChecks, Maximize2, Settings2 } from "lucide-react";

export const navigationTools = [
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
