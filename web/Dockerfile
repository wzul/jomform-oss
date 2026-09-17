# JomForm web — Next.js 16 standalone build.
#
# THE BUILD CONTEXT IS THE REPO ROOT, NOT web/:
#
#     docker build -f web/Dockerfile .
#     IMAGE=jomform-web CONTEXT=. DOCKERFILE=web/Dockerfile ./deploy/build-and-ship.sh vX.Y.Z
#
# deploy/DEPLOY.md documents that command; every COPY source below is written
# against the root context, and the root `.dockerignore` keeps the uploaded
# context to source (without it the daemon gets ~1 GB of node_modules/.next,
# and `COPY web/ ./` would clobber the tree `npm ci` just installed).
#
# WHY THE CONTEXT IS THE ROOT: `npm run build` runs the unit tests first
# (web/package.json) and web/src/lib/adminGrowth.test.ts reads
# api/internal/dbstore/admin_overview.go — the Go struct behind the /admin
# growth JSON — via a web-relative path. That cross-language read is the whole
# point of the guard (it pins the /admin legend order to the API's field order
# rather than restating it in the test), so the context must contain api/ as a
# sibling of web/. Building from web/ made the read resolve to
# /api/internal/dbstore/admin_overview.go, an unhandled ENOENT, so `next build`
# never ran and the image build exited 1.
#
# The container mirrors the checkout layout (/repo/web + /repo/api) so a
# web-relative path resolves inside the image exactly as it does on a
# developer's machine. Nothing from api/ reaches the runtime stage below.
FROM node:24-alpine AS build
WORKDIR /repo/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY api/ /repo/api/
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:24-alpine
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000
COPY --from=build /repo/web/.next/standalone ./
COPY --from=build /repo/web/.next/static ./.next/static
COPY --from=build /repo/web/public ./public
EXPOSE 3000
CMD ["node", "server.js"]
