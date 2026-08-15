import { STAGE_STYLES, cx } from "../lib/display";
import type { Stage } from "../lib/types";
import { Badge } from "./ui";

/**
 * The coloured pill that identifies a lead's stage.
 *
 * Used in the table, the detail header, the dashboard's recent list and the
 * timeline, all from this one component -- so "Qualified" is the same violet
 * everywhere and the colour becomes something the eye can rely on.
 */
export function StageBadge({
    stage,
    showIcon = true,
    className,
}: {
    stage: Stage;
    showIcon?: boolean;
    className?: string;
}) {
    const style = STAGE_STYLES[stage];
    const Icon = style.icon;

    return (
        <Badge tone={style.badge} className={cx("whitespace-nowrap", className)}>
            {showIcon && <Icon className="size-3" aria-hidden="true" />}
            {style.label}
        </Badge>
    );
}
