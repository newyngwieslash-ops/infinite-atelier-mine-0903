import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";
import { STUDIO_SECTIONS, type StudioSection } from "@/stores/use-studio-store";

/**
 * StudioSectionNav is the studio shell's left-hand navigation.
 *
 * It renders STUDIO_SECTIONS as it stands, including the sections no work
 * package has filled yet: each of those carries its owning work package as a
 * badge, so a viewer reads "WP-09" rather than an empty page and guessing. The
 * list itself is not defined here — `use-studio-store.ts` owns the order and the
 * keys, and this component is only a view over it.
 *
 * The active entry is marked three ways (background, weight, and
 * `aria-current="true"`), because a state that only changes colour is invisible
 * to a screen reader and to a greyscale print of the screen.
 */
export type StudioSectionNavProps = {
    active: StudioSection;
    onSelect: (section: StudioSection) => void;
};

export function StudioSectionNav({ active, onSelect }: StudioSectionNavProps) {
    const { t } = useTranslation();

    return (
        <nav className="flex flex-col gap-1" aria-label={t("studio.navLabel")} data-testid="studio-section-nav">
            {STUDIO_SECTIONS.map((spec) => {
                const current = spec.id === active;
                return (
                    <button
                        key={spec.id}
                        type="button"
                        // These two attributes are the contract the end-to-end test
                        // selects on: the visible label is translated, so it cannot
                        // be the anchor.
                        data-section={spec.id}
                        data-section-available={spec.available}
                        aria-current={current ? "true" : undefined}
                        onClick={() => onSelect(spec.id)}
                        className={cn(
                            "flex w-full flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-left transition",
                            current
                                ? "bg-stone-100 font-medium text-stone-950 dark:bg-stone-800 dark:text-stone-100"
                                : "text-stone-600 hover:bg-stone-100 hover:text-stone-950 dark:text-stone-300 dark:hover:bg-stone-800 dark:hover:text-stone-100",
                        )}
                    >
                        <span className="flex w-full items-center justify-between gap-2">
                            <span className="text-sm leading-6">{t(spec.titleKey)}</span>
                            {!spec.available ? (
                                // The badge names the package that will fill the section,
                                // so the gap is attributable rather than anonymous.
                                <span
                                    className="shrink-0 rounded border border-stone-300 px-1.5 py-0.5 text-[10px] uppercase leading-none text-stone-500 dark:border-stone-600 dark:text-stone-400"
                                    title={t("studio.sectionStatus.ownedBy", { workPackage: spec.workPackage })}
                                >
                                    {spec.workPackage}
                                </span>
                            ) : null}
                        </span>
                    </button>
                );
            })}
        </nav>
    );
}
