import assert from "node:assert/strict";
import test from "node:test";

import {
  addPoly,
  emptyNoGoDoc,
  parseNoGoDoc,
  polyContains,
  polyOnLayer,
  removePoly,
  serializeNoGoDoc,
  zonePolys,
} from "./static/js/nogo_zones.js";

const SQUARE = [
  [0, 0],
  [10, 0],
  [10, 10],
  [0, 10],
];

test("parse keeps only rings that enclose an area and normalizes tiers", () => {
  const doc = parseNoGoDoc({
    version: 1,
    zones: {
      map02base: [
        {id: "a", poly: SQUARE, tier: " Wuling_L2_254 "},
        {
          poly: [
            [1, 1],
            [2, 2],
          ],
        },
        {
          poly: [
            ["3", "4"],
            [5, 6],
            [7, "x"],
            [9, 10],
          ],
          tier: "",
        },
      ],
      empty: [],
    },
  });
  assert.deepEqual(Object.keys(doc.zones), ["map02base"]);
  const [onTier, everyLayer] = doc.zones.map02base;
  assert.equal(onTier.tier, "Wuling_L2_254");
  assert.deepEqual(everyLayer.ring, [
    [3, 4],
    [5, 6],
    [9, 10],
  ]);
  assert.equal(everyLayer.id, "");
  assert.equal(everyLayer.tier, null);
  assert.deepEqual(parseNoGoDoc(null), emptyNoGoDoc());
});

test("addPoly assigns ids that stay unique inside the zone", () => {
  const doc = emptyNoGoDoc();
  assert.equal(
    addPoly(doc, "map02base", [
      [0, 0],
      [1, 1],
    ]),
    null,
  );
  const first = addPoly(doc, "map02base", SQUARE);
  assert.equal(first.id, "nogo-1");
  assert.equal(first.tier, null);
  assert.ok(removePoly(doc, "map02base", 0));
  assert.deepEqual(doc.zones, {});
  addPoly(doc, "map02base", SQUARE);
  doc.zones.map02base[0].id = "nogo-2";
  assert.equal(addPoly(doc, "map02base", SQUARE, "Wuling_L2_254").id, "nogo-3");
  assert.equal(doc.zones.map02base[1].tier, "Wuling_L2_254");
  assert.equal(zonePolys(doc, "other").length, 0);
});

test("polyOnLayer shows every-layer polygons everywhere and tier ones only on their tier", () => {
  const everyLayer = {tier: null};
  const onTier = {tier: "Wuling_L2_254"};
  assert.ok(polyOnLayer(everyLayer, null));
  assert.ok(polyOnLayer(everyLayer, "Wuling_L5_318"));
  assert.ok(polyOnLayer(onTier, null));
  assert.ok(polyOnLayer(onTier, "Wuling_L2_254"));
  assert.ok(!polyOnLayer(onTier, "Wuling_L5_318"));
});

test("polyContains follows the even-odd rule on a self-intersecting ring", () => {
  assert.ok(polyContains(SQUARE, 5, 5));
  assert.ok(!polyContains(SQUARE, 15, 5));
  const bowtie = [
    [0, 0],
    [10, 10],
    [10, 0],
    [0, 10],
  ];
  assert.ok(polyContains(bowtie, 2, 5));
  assert.ok(!polyContains(bowtie, 5, 2));
});

test("serialize writes the planner's exact shape with rounded numbers", () => {
  const doc = emptyNoGoDoc();
  addPoly(
    doc,
    "map02base",
    [
      [0.12345, 0],
      [10, 0.0004],
      [10, 10],
    ],
    "Wuling_L2_254",
  );
  addPoly(doc, "map02base", SQUARE);
  assert.deepEqual(serializeNoGoDoc(doc), {
    version: 1,
    zones: {
      map02base: [
        {
          poly: [
            [0.123, 0],
            [10, 0],
            [10, 10],
          ],
          id: "nogo-1",
          tier: "Wuling_L2_254",
        },
        {poly: SQUARE, id: "nogo-2"},
      ],
    },
  });
});
