import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { GoalDetailPage as GoalDetail } from "@multica/views/goals/components";
import { useWorkspaceId } from "@multica/core/hooks";
import { goalDetailOptions } from "@multica/core/goals";
import { useDocumentTitle } from "@/hooks/use-document-title";

/**
 * Desktop wrapper: react-router owns the path param, the shared view owns the
 * page. The title is set here because only the shell has a window to name.
 */
export function GoalDetailRoute() {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: goal } = useQuery(goalDetailOptions(wsId, id ?? ""));

  useDocumentTitle(goal?.title ? goal.title : "Goal");

  if (!id) return null;
  return <GoalDetail goalId={id} />;
}
