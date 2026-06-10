import { cn } from "@/lib/utils";

interface SkillListProps {
  title: string;
  skills: string[];
  tone: "positive" | "warning";
  emptyHint: string;
}

export function SkillList({ title, skills, tone, emptyHint }: SkillListProps) {
  const dot = tone === "positive" ? "bg-positive" : "bg-warning";

  return (
    <div>
      <h4 className="text-xs font-medium uppercase tracking-[0.12em] text-ink-faint">
        {title}
      </h4>
      {skills.length === 0 ? (
        <p className="mt-2 text-sm text-ink-faint">{emptyHint}</p>
      ) : (
        <div className="mt-3 flex flex-wrap gap-2">
          {skills.map((skill) => (
            <span key={skill} className="chip border-line text-ink-muted">
              <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", dot)} />
              <span className="min-w-0 break-words">{skill}</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
