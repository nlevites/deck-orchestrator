export type SampleCategory = "topology" | "validation" | "stress";

export interface SampleDag {
  id: string;
  label: string;
  description: string;
  topology: string;
  category: SampleCategory;
  json: object;
  /** Set on validation samples so the picker can label them and the UI can
   * skip auto-validation noise — selecting them populates the editor and
   * lets the inline validator surface the expected error code. */
  expectInvalid?: boolean;
  /** Short mono badge shown to the right of the sample label (validator
   * code for invalid samples, sizing hint for stress samples). */
  badge?: string;
}
