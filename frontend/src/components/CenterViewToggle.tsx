import './CenterViewToggle.css';

export type CenterView = 'list' | 'floor' | 'health';

export function CenterViewToggle({ view, onChange }: { view: CenterView; onChange: (view: CenterView) => void }) {
  return (
    <div className="center-view-toggle" role="tablist" aria-label="Center view">
      <button type="button" role="tab" aria-selected={view === 'list'} data-active={view === 'list'} onClick={() => onChange('list')}>
        list
      </button>
      <button type="button" role="tab" aria-selected={view === 'floor'} data-active={view === 'floor'} onClick={() => onChange('floor')}>
        floor
      </button>
      <button type="button" role="tab" aria-selected={view === 'health'} data-active={view === 'health'} onClick={() => onChange('health')}>
        health
      </button>
    </div>
  );
}
