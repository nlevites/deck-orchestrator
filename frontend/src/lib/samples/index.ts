import { invalidSamples } from "./invalid";
import { largeSamples } from "./large";
import { manyStepsSamples } from "./many-steps";
import type { SampleCategory, SampleDag } from "./types";
import { validSamples } from "./valid";

export type { SampleCategory, SampleDag };

export const SAMPLE_DAGS: SampleDag[] = [
  ...validSamples,
  ...invalidSamples,
  ...largeSamples,
  ...manyStepsSamples,
];
