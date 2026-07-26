'use client';

import React from 'react';

const TUTORIAL_DONE_KEY = 'chess404_tutorial_done';

export type TutorialStep = 'welcome' | 'board' | 'cards' | 'mana' | 'spells' | 'complete';

export interface TutorialState {
  active: boolean;
  step: TutorialStep;
  dismiss: () => void;
  next: () => void;
  prev: () => void;
  open: () => void;
}

export function useTutorial(): TutorialState {
  const [active, setActive] = React.useState(() => {
    if (typeof window === 'undefined') return false;
    return localStorage.getItem(TUTORIAL_DONE_KEY) !== 'true';
  });
  const [step, setStep] = React.useState<TutorialStep>('welcome');

  const finish = React.useCallback(() => {
    setActive(false);
    setStep('complete');
    if (typeof window !== 'undefined') {
      localStorage.setItem(TUTORIAL_DONE_KEY, 'true');
    }
  }, []);

  const open = React.useCallback(() => {
    setStep('welcome');
    setActive(true);
  }, []);

  return {
    active,
    step,
    dismiss: finish,
    open,
    next: () => {
      if (step === 'welcome') setStep('board');
      else if (step === 'board') setStep('cards');
      else if (step === 'cards') setStep('mana');
      else if (step === 'mana') setStep('spells');
      else if (step === 'spells') finish();
    },
    prev: () => {
      if (step === 'board') setStep('welcome');
      else if (step === 'cards') setStep('board');
      else if (step === 'mana') setStep('cards');
      else if (step === 'spells') setStep('mana');
    },
  };
}
