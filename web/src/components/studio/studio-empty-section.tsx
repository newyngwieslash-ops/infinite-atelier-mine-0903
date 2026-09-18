import { useTranslation } from "react-i18next";

/**
 * StudioEmptySection states that a section has no implementation yet.
 *
 * It deliberately renders no rows, no counts and no greyed-out placeholders: a
 * plausible-looking skeleton would be indistinguishable from a section that is
 * built and happens to be empty, which is the confusion this component exists to
 * prevent. What it shows instead is the one fact a viewer needs — which work
 * package owns the section — plus the section's own one-line description.
 *
 * The work package is printed as its identifier ("WP-09") rather than a
 * translated phrase, because it is a document reference, not prose.
 */
export type StudioEmptySectionProps = {
    /** i18n key for the section title. */
    titleKey: string;
    /** i18n key for the one-line description of what the section will hold. */
    descriptionKey: string;
    /** The work package that owns this section's content. */
    workPackage: string;
};

export function StudioEmptySection({ titleKey, descriptionKey, workPackage }: StudioEmptySectionProps) {
    const { t } = useTranslation();

    return (
        <section
            data-empty-section="true"
            data-empty-section-work-package={workPackage}
            className="flex min-h-[280px] flex-col items-start justify-center rounded-xl border border-dashed border-stone-300 px-8 py-10 dark:border-stone-700"
        >
            <p className="text-xs text-stone-500">{t("studio.sectionStatus.planned")}</p>
            <h2 className="mt-3 text-xl font-medium">{t(titleKey)}</h2>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-stone-600 dark:text-stone-400">{t(descriptionKey)}</p>
            <p className="mt-4 text-sm text-stone-500" data-empty-section-work-package-label>
                {t("studio.sectionStatus.ownedBy", { workPackage })}
            </p>
        </section>
    );
}
