import React from 'react';
import ReactDOM from 'react-dom/client';
import { App } from './App';
import './styles/tokens.css';
import './styles/global.css';

interface ErrorBoundaryState {
  error?: Error;
}

class ErrorBoundary extends React.Component<React.PropsWithChildren, ErrorBoundaryState> {
  state: ErrorBoundaryState = {};

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error) {
    console.error('Hikari render failed', error);
  }

  render() {
    if (this.state.error) return <StartupError error={this.state.error} />;
    return this.props.children;
  }
}

function StartupError(props: { error: Error }) {
  return (
    <div className="startup-error">
      <strong>Hikari could not load</strong>
      <span>{props.error.message}</span>
    </div>
  );
}

const root = document.getElementById('root');

if (!root) {
  document.body.innerHTML = '<div class="startup-error"><strong>Hikari could not load</strong><span>Missing application root.</span></div>';
} else {
  ReactDOM.createRoot(root).render(
    <React.StrictMode>
      <ErrorBoundary>
        <App />
      </ErrorBoundary>
    </React.StrictMode>,
  );
}
