interface PageHeaderProps {
  title: string;
  body?: string;
  actions?: React.ReactNode;
}

export function PageHeader({ title, body, actions }: PageHeaderProps) {
  return (
    <header className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
      <div className="flex min-w-0 flex-col gap-1.5">
        <h1 className="text-section-sm font-semibold tracking-section text-ink md:text-section">
          {title}
        </h1>
        {body && (
          <p className="max-w-container-small text-[15px] tracking-sub text-ink-muted">{body}</p>
        )}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}
