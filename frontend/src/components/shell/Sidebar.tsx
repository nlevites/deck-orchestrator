import { NavLink } from "react-router-dom";
import { LayoutDashboard, Cpu, Workflow, Send, Settings, type LucideIcon } from "lucide-react";
import { cn } from "@/lib/cn";

interface Item {
  to: string;
  label: string;
  icon: LucideIcon;
}

const items: Item[] = [
  { to: "/fleet", label: "Fleet", icon: LayoutDashboard },
  { to: "/fleet/grid", label: "Decks", icon: Cpu },
  { to: "/runs", label: "Runs", icon: Workflow },
  { to: "/submit", label: "New run", icon: Send },
  { to: "/settings", label: "Settings", icon: Settings },
];

/** Icon-only rail; labels reveal on hover. Hidden below md. */
export function Sidebar() {
  return (
    <aside
      className={cn(
        "group/sidebar sticky top-0 hidden h-[calc(100vh-2.5rem)] shrink-0 flex-col border-r border-line bg-surface",
        "w-14 transition-[width] duration-200 ease-out-soft hover:w-48",
        "md:flex",
      )}
      aria-label="Console navigation"
    >
      <nav className="flex flex-col gap-0.5 p-2">
        {items.map((item) => (
          <SidebarLink key={item.to} {...item} />
        ))}
      </nav>
    </aside>
  );
}

function SidebarLink({ to, label, icon: Icon }: Item) {
  return (
    <NavLink
      to={to}
      title={label}
      aria-label={label}
      className={({ isActive }) =>
        cn(
          "flex h-10 items-center gap-3 rounded-md px-2.5 text-[14px] font-medium tracking-nav transition-colors duration-150 ease-out-soft",
          isActive ? "bg-line/70 text-ink" : "text-ink-nav hover:bg-line/40 hover:text-ink",
        )
      }
      end={to === "/fleet"}
    >
      <Icon size={18} strokeWidth={1.7} className="shrink-0" />
      <span className="overflow-hidden whitespace-nowrap opacity-0 transition-opacity duration-150 ease-out-soft group-hover/sidebar:opacity-100">
        {label}
      </span>
    </NavLink>
  );
}
