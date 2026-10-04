import { isLocalRequest } from '../../_lib/internal-service';
import { proxyRealtime } from '../_lib/proxy';

export const dynamic = 'force-dynamic';

export async function POST(request: Request): Promise<Response> {
  if (!isLocalRequest(request)) {
    return Response.json({
      error: 'direct match creation is not public; use the gateway match flow',
    }, { status: 404 });
  }
  return proxyRealtime(request, '/api/matches');
}
