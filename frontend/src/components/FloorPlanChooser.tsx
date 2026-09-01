import { ChevronRight, Plus } from 'lucide-react';
import type { FloorPlanProfile, FloorPlanProfileCandidate, FloorPlanProfileResolution } from '../domain/floorPlanProfiles.js';
import { CenterViewToggle, type CenterView } from './CenterViewToggle';
import './FloorPlan.css';

interface FloorPlanChooserProps {
  profiles: FloorPlanProfile[];
  resolution: FloorPlanProfileResolution;
  view: CenterView;
  onViewChange: (view: CenterView) => void;
  onSelect: (profileId: string) => void;
  onCreate: () => void;
}

export function FloorPlanChooser({ profiles, resolution, view, onViewChange, onSelect, onCreate }: FloorPlanChooserProps) {
  const candidates = new Map(resolution.candidates.map((candidate) => [candidate.profileId, candidate]));
  const title = resolution.kind === 'ambiguous' ? 'choose a floor plan' : profiles.length ? 'select a floor plan' : 'create a floor plan';
  return (
    <main className="center-panel">
      <div className="floor-plan-shell floor-plan-choice-shell">
        <header className="floor-plan-header">
          <div>
            <span>floor plans</span>
            <h1>{title}</h1>
          </div>
          <CenterViewToggle view={view} onChange={onViewChange} />
        </header>
        <div className="floor-plan-choice-list">
          {profiles.map((profile) => (
            <button key={profile.id} type="button" onClick={() => onSelect(profile.id)}>
              <span>
                <strong>{profile.name}</strong>
                <small>{candidateDescription(candidates.get(profile.id))}</small>
              </span>
              <ChevronRight size={14} aria-hidden="true" />
            </button>
          ))}
          <button type="button" onClick={onCreate}>
            <span>
              <strong>New floor plan</strong>
              <small>Use the devices currently available on this LAN</small>
            </span>
            <Plus size={14} aria-hidden="true" />
          </button>
        </div>
      </div>
    </main>
  );
}

function candidateDescription(candidate: FloorPlanProfileCandidate | undefined): string {
  if (!candidate) return 'No known devices found';
  const devices = candidate.serialMatches;
  if (devices > 0) return `${devices} known device${devices === 1 ? '' : 's'} found`;
  const locations = candidate.locationMatches;
  return `${locations} known location${locations === 1 ? '' : 's'} found`;
}
