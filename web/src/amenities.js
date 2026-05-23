// Fallback canonical amenity codes, mirroring the backend's
// property.CanonicalAmenities. The live list is fetched from GET /amenities;
// this is used as the initial value and a fallback if the request fails.
export const AMENITY_CODES = [
  'wifi',
  'kitchen',
  'parking',
  'pool',
  'air-conditioning',
  'heating',
  'washer',
  'dryer',
  'tv',
  'workspace',
  'hot-tub',
  'gym',
  'pets-allowed',
  'elevator',
  'breakfast',
  'smoke-alarm',
];
