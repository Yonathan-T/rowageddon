import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '5s', target: 50 },
    { duration: '15s', target: 100 },
    { duration: '5s', target: 0 },
  ],
  thresholds: {
    http_req_failed: ['rate<0.01'],    // Error rate must be less than 1%
    http_req_duration: ['p(95)<100'],  // 95% of requests must complete below 100ms
  },
};

const BASE_URL = 'http://127.0.0.1:8080';

const sampleAuthors = [
  'drPeters',
  'fewJonathan',
  'chest766',
  'Janet_Lopez',
  'franticmouse'
];

export default function () {
  const rand = Math.random();

  if (rand < 0.40) {
    const res = http.get(`${BASE_URL}/api/stories?limit=10&days=30`, { tags: { name: '/api/stories' } });
    check(res, { 'stories status is 200': (r) => r.status === 200 });
  } else if (rand < 0.70) {
    const randomRowID = Math.floor(Math.random() * 95000000) + 1;
    const res = http.get(`${BASE_URL}/api/raw?from_id=${randomRowID}&limit=25`, { tags: { name: '/api/raw' } });
    check(res, { 'raw cursor status is 200': (r) => r.status === 200 });
  } else if (rand < 0.90) {
    const author = sampleAuthors[Math.floor(Math.random() * sampleAuthors.length)];
    const res = http.get(`${BASE_URL}/api/user/${author}?limit=20`, { tags: { name: '/api/user/:author' } });
    check(res, { 'user lookup status is 200': (r) => r.status === 200 });
  } else {
    const randomParentID = Math.floor(Math.random() * 50000000) + 100;
    const res = http.get(`${BASE_URL}/api/item/${randomParentID}/comments`, { tags: { name: '/api/item/:id/comments' } });
    check(res, { 'comments status is 200': (r) => r.status === 200 });
  }

  sleep(0.02);
}
