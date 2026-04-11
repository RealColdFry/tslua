import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  override state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("Playground crashed:", error, info.componentStack);
  }

  override render(): ReactNode {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div className="pg-crash">
        <h2>Playground crashed</h2>
        <p>Something went wrong while rendering the playground.</p>
        <pre className="pg-crash-msg">{error.message}</pre>
        {error.stack && <pre className="pg-crash-stack">{error.stack}</pre>}
        <button
          type="button"
          className="pg-crash-reset"
          onClick={() => this.setState({ error: null })}
        >
          Reset
        </button>
      </div>
    );
  }
}
