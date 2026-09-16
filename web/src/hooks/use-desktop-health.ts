import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { getDesktopHealth, isDesktopHealthBindingAvailable, onHealthChanged } from "@/services/desktop/health";

export const desktopHealthQueryKey = ["desktop-health"] as const;

export function useDesktopHealth() {
    const queryClient = useQueryClient();
    const desktopAvailable = isDesktopHealthBindingAvailable();
    const query = useQuery({
        queryKey: desktopHealthQueryKey,
        queryFn: getDesktopHealth,
        enabled: desktopAvailable,
    });

    useEffect(() => {
        if (!desktopAvailable) return () => undefined;

        return onHealthChanged(() => {
            void queryClient.invalidateQueries({ queryKey: desktopHealthQueryKey });
        });
    }, [desktopAvailable, queryClient]);

    return {
        ...query,
        data: desktopAvailable ? query.data : null,
    };
}
