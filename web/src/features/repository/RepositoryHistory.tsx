import { Empty } from "../../components/Feedback";
import type { Commit, Repository } from "./types";

export function RepositoryHistory({
  repo,
  commits,
}: {
  repo: Repository | null;
  commits: Commit[];
}) {
  return (
    <div class="detail-content">
      <h1>Repository</h1>
      <p class="muted">
        Branch {repo?.branch || "not configured"} · remote origin
      </p>
      <h2>History</h2>
      {commits.map((commit) => (
        <article class="commit-row" key={commit.sha}>
          <code>{commit.sha.slice(0, 7)}</code>
          <div>
            <strong>{commit.subject}</strong>
            <p class="muted">
              {commit.author} · {commit.time}
            </p>
          </div>
        </article>
      ))}
      {!commits.length && <Empty>No commits available.</Empty>}
    </div>
  );
}
