import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  vus: 5,
  duration: '30s',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500'],
    checks: ['rate==1'],
  },
};

// Each VU owns its own tree, ids offset so trees never collide.
function tree(vu, iter) {
  const base = 1000000 + vu * 1000;
  const leaves = [];
  for (let i = 0; i < 20; i++) {
    leaves.push({ id: base + 10 + i, type: 'resource_group', children: [] });
  }
  // Alternate the parent of the leaves so every POST also moves nodes.
  const subs = [
    { id: base + 1, type: 'subscription', children: iter % 2 === 0 ? leaves : [] },
    { id: base + 2, type: 'subscription', children: iter % 2 === 0 ? [] : leaves },
  ];
  return { id: base, type: 'management_group', children: subs };
}

export default function () {
  const body = tree(__VU, __ITER);
  const payload = JSON.stringify(body);
  const post = http.post(`${BASE_URL}/hierarchy`, payload, {
    headers: { 'Content-Type': 'application/json' },
  });
  check(post, { 'POST 200': (r) => r.status === 200 });

  const get = http.get(`${BASE_URL}/hierarchy/${body.id}`);
  check(get, {
    'GET 200': (r) => r.status === 200,
    'GET returns the posted tree': (r) => r.body === payload,
  });
}
