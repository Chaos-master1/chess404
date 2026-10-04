import { isLocalRequest } from '../../_lib/internal-service';

export const dynamic = 'force-dynamic';

export async function POST(request: Request): Promise<Response> {
  if (isLocalRequest(request)) {
    return Response.json({
      error: 'client-side account result finalization is disabled; use the trusted backend finalizer',
    }, { status: 403 });
  }
  return Response.json({
    error: 'client-side account result finalization is disabled',
  }, { status: 404 });
}
