import { proxyPlatform } from '../../_lib/proxy';

export const dynamic = 'force-dynamic';

export async function GET(
  request: Request,
  context: { params: Promise<{ matchId: string }> }
): Promise<Response> {
  const { matchId } = await context.params;
  // Forward the query string: the replay endpoint reads guestId from it to let
  // a participant open their own vs-computer or private game.
  const { search } = new URL(request.url);
  return proxyPlatform(request, `/api/platform/matches/${matchId}${search}`);
}
