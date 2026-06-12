import type { Locale } from "@/lib/i18n/config";

export interface Dictionary {
  nav: {
    signIn: string;
    openDashboard: string;
    signOut: string;
  };
  landing: {
    eyebrow: string;
    title: string;
    subtitle: string;
    ctaPrimary: string;
    seeHow: string;
    freeNote: string;
    stepsTitle: string;
    steps: { title: string; body: string }[];
    footerTagline: string;
    preview: {
      role: string;
      verdict: string;
      matched: string;
      missing: string;
    };
  };
  dashboard: {
    title: string;
    subtitle: string;
    cvLabel: string;
    roleLabel: string;
    optional: string;
    rolePlaceholder: string;
    jobLabel: string;
    jobPlaceholder: string;
    analyze: string;
    analyzing: string;
    dropTitle: string;
    dropHint: string;
    errPdfOnly: string;
    errTooLarge: string;
    removeFile: string;
    reuseCvPlaceholder: string;
    reusedCvHint: string;
    historyTitle: string;
    historyEmpty: string;
    historyLoading: string;
    historySearch: string;
    historyNoMatches: string;
    filterAll: string;
    untitledRole: string;
    deleteAnalysis: string;
    confirmDelete: {
      title: string;
      description: string;
      confirm: string;
      cancel: string;
    };
  };
  result: {
    emptyTitle: string;
    emptyBody: string;
    loadingTitle: string;
    loadingBody: string;
    errorTitle: string;
    errorGeneric: string;
    sessionExpired: string;
    cvNotice: string;
    back: string;
    errors: {
      invalidRequest: string;
      invalidJobOffer: string;
      cvRequired: string;
      unsupportedMediaType: string;
      payloadTooLarge: string;
      uploadError: string;
      unreadableCv: string;
      analysisFailed: string;
      upstreamTimeout: string;
      upstreamUnavailable: string;
      networkError: string;
    };
  };
  analysis: {
    fit: string;
    summary: string;
    matched: string;
    missing: string;
    improve: string;
    matchedEmpty: string;
    missingEmpty: string;
    downloadCv: string;
    defaultTitle: string;
    verdict: { strong: string; moderate: string; weak: string };
    priority: { high: string; medium: string; low: string };
  };
  a11y: {
    toggleTheme: string;
    switchLanguage: string;
  };
}

const en: Dictionary = {
  nav: {
    signIn: "Sign in",
    openDashboard: "Open dashboard",
    signOut: "Sign out",
  },
  landing: {
    eyebrow: "Resume intelligence",
    title: "Your CV, matched to the role.",
    subtitle:
      "Upload your resume and a job posting. Papyrus scores the fit from 0 to 100, names every gap, and tells you exactly what to change — before you hit apply.",
    ctaPrimary: "Analyze my CV",
    seeHow: "See how it works",
    freeNote: "Free to use. Sign in with Google — no card required.",
    stepsTitle: "Three steps to a sharper application.",
    steps: [
      {
        title: "Upload your CV",
        body: "Drop in a PDF. We read it the way a hiring manager skims it — for signal, not formatting.",
      },
      {
        title: "Paste the posting",
        body: "Any job description works. Papyrus pulls out what the role actually requires.",
      },
      {
        title: "Read the verdict",
        body: "A score, the gaps that matter, and the exact edits that would close them.",
      },
    ],
    footerTagline: "Resume intelligence, kept simple.",
    preview: {
      role: "Senior Backend Engineer",
      verdict: "Strong fit",
      matched: "Matched",
      missing: "Missing",
    },
  },
  dashboard: {
    title: "New analysis",
    subtitle: "Score your CV against a posting.",
    cvLabel: "Your CV",
    roleLabel: "Role",
    optional: "Optional",
    rolePlaceholder: "e.g. Senior Backend Engineer",
    jobLabel: "Job posting",
    jobPlaceholder: "Paste the full job description here…",
    analyze: "Analyze fit",
    analyzing: "Analyzing…",
    dropTitle: "Drop your CV or browse",
    dropHint: "PDF · up to {mb} MB",
    errPdfOnly: "Only PDF files are supported.",
    errTooLarge: "The file exceeds the {mb} MB limit.",
    removeFile: "Remove file",
    reuseCvPlaceholder: "Or reuse a previous CV…",
    reusedCvHint: "Reusing a saved CV",
    historyTitle: "History",
    historyEmpty: "Your past analyses will appear here.",
    historyLoading: "Loading…",
    historySearch: "Search by role",
    historyNoMatches: "No analyses match your filters.",
    filterAll: "All fits",
    untitledRole: "Untitled role",
    deleteAnalysis: "Delete analysis",
    confirmDelete: {
      title: "Delete this analysis?",
      description:
        "This permanently removes the analysis and its CV. This can’t be undone.",
      confirm: "Delete",
      cancel: "Cancel",
    },
  },
  result: {
    emptyTitle: "Ready when you are",
    emptyBody:
      "Add your CV and a job posting, and your compatibility breakdown will appear here.",
    loadingTitle: "Reading your CV",
    loadingBody:
      "Weighing your experience against what the role asks for. This takes a few seconds.",
    errorTitle: "That didn’t work",
    errorGeneric: "Something went wrong while analyzing. Please try again.",
    sessionExpired: "Your session has expired. Please sign in again.",
    cvNotice:
      "We couldn’t save your CV, so it won’t be downloadable from your history.",
    back: "Back",
    errors: {
      invalidRequest: "The request was malformed or missing required fields.",
      invalidJobOffer: "The job posting is too short to analyze.",
      cvRequired: "A CV file is required.",
      unsupportedMediaType: "Only PDF files are supported.",
      payloadTooLarge: "The CV exceeds the {mb} MB limit.",
      uploadError: "The CV file could not be read.",
      unreadableCv: "The CV is not a readable PDF.",
      analysisFailed: "The analysis could not be completed.",
      upstreamTimeout: "The analysis took too long. Please try again.",
      upstreamUnavailable: "The analysis service is temporarily unavailable.",
      networkError: "Couldn't reach the analysis service. Check your connection and try again.",
    },
  },
  analysis: {
    fit: "Fit",
    summary: "Summary",
    matched: "Matched skills",
    missing: "Missing skills",
    improve: "How to improve",
    matchedEmpty: "No direct overlaps were found.",
    missingEmpty: "Nothing critical is missing.",
    downloadCv: "Download CV",
    defaultTitle: "Compatibility analysis",
    verdict: { strong: "Strong fit", moderate: "Moderate fit", weak: "Weak fit" },
    priority: { high: "High impact", medium: "Worth doing", low: "Nice to have" },
  },
  a11y: {
    toggleTheme: "Toggle theme",
    switchLanguage: "Switch language",
  },
};

const es: Dictionary = {
  nav: {
    signIn: "Iniciar sesión",
    openDashboard: "Abrir panel",
    signOut: "Cerrar sesión",
  },
  landing: {
    eyebrow: "Inteligencia para tu CV",
    title: "Tu CV, medido contra el puesto.",
    subtitle:
      "Subí tu CV y una oferta de trabajo. Papyrus mide la compatibilidad del 0 al 100, detecta cada brecha y te dice exactamente qué cambiar — antes de postularte.",
    ctaPrimary: "Analizar mi CV",
    seeHow: "Cómo funciona",
    freeNote: "Gratis. Iniciá sesión con Google — sin tarjeta.",
    stepsTitle: "Tres pasos hacia una postulación más afilada.",
    steps: [
      {
        title: "Subí tu CV",
        body: "Soltá un PDF. Lo leemos como lo escanea un reclutador — buscando señales, no formato.",
      },
      {
        title: "Pegá la oferta",
        body: "Sirve cualquier descripción. Papyrus extrae lo que el puesto realmente pide.",
      },
      {
        title: "Leé el veredicto",
        body: "Un score, las brechas que importan y los cambios exactos para cerrarlas.",
      },
    ],
    footerTagline: "Inteligencia para tu CV, simple.",
    preview: {
      role: "Ingeniero Backend Senior",
      verdict: "Alta compatibilidad",
      matched: "Coinciden",
      missing: "Faltan",
    },
  },
  dashboard: {
    title: "Nuevo análisis",
    subtitle: "Medí tu CV contra una oferta.",
    cvLabel: "Tu CV",
    roleLabel: "Puesto",
    optional: "Opcional",
    rolePlaceholder: "ej. Ingeniero Backend Senior",
    jobLabel: "Oferta de trabajo",
    jobPlaceholder: "Pegá la descripción completa del puesto acá…",
    analyze: "Analizar compatibilidad",
    analyzing: "Analizando…",
    dropTitle: "Soltá tu CV o elegí un archivo",
    dropHint: "PDF · hasta {mb} MB",
    errPdfOnly: "Solo se admiten archivos PDF.",
    errTooLarge: "El archivo supera el límite de {mb} MB.",
    removeFile: "Quitar archivo",
    reuseCvPlaceholder: "O reusá un CV anterior…",
    reusedCvHint: "Reutilizando un CV guardado",
    historyTitle: "Historial",
    historyEmpty: "Tus análisis anteriores van a aparecer acá.",
    historyLoading: "Cargando…",
    historySearch: "Buscar por puesto",
    historyNoMatches: "Ningún análisis coincide con los filtros.",
    filterAll: "Todas",
    untitledRole: "Puesto sin título",
    deleteAnalysis: "Eliminar análisis",
    confirmDelete: {
      title: "¿Eliminar este análisis?",
      description:
        "Esto elimina el análisis y su CV de forma permanente. No se puede deshacer.",
      confirm: "Eliminar",
      cancel: "Cancelar",
    },
  },
  result: {
    emptyTitle: "Cuando quieras",
    emptyBody:
      "Agregá tu CV y una oferta, y acá va a aparecer tu análisis de compatibilidad.",
    loadingTitle: "Leyendo tu CV",
    loadingBody:
      "Comparando tu experiencia con lo que pide el puesto. Tarda unos segundos.",
    errorTitle: "No funcionó",
    errorGeneric: "Algo salió mal durante el análisis. Probá de nuevo.",
    sessionExpired: "Tu sesión expiró. Iniciá sesión de nuevo.",
    cvNotice:
      "No pudimos guardar tu CV, así que no vas a poder descargarlo desde el historial.",
    back: "Volver",
    errors: {
      invalidRequest: "La solicitud es inválida o le faltan campos obligatorios.",
      invalidJobOffer: "La oferta de trabajo es muy corta para analizarla.",
      cvRequired: "Se requiere un archivo de CV.",
      unsupportedMediaType: "Solo se admiten archivos PDF.",
      payloadTooLarge: "El CV supera el límite de {mb} MB.",
      uploadError: "No se pudo leer el archivo del CV.",
      unreadableCv: "El CV no es un PDF legible.",
      analysisFailed: "No se pudo completar el análisis.",
      upstreamTimeout: "El análisis tardó demasiado. Probá de nuevo.",
      upstreamUnavailable: "El servicio de análisis no está disponible en este momento.",
      networkError: "No pudimos conectar con el servicio de análisis. Revisá tu conexión y probá de nuevo.",
    },
  },
  analysis: {
    fit: "Ajuste",
    summary: "Resumen",
    matched: "Habilidades que coinciden",
    missing: "Habilidades faltantes",
    improve: "Cómo mejorar",
    matchedEmpty: "No se encontraron coincidencias directas.",
    missingEmpty: "No falta nada crítico.",
    downloadCv: "Descargar CV",
    defaultTitle: "Análisis de compatibilidad",
    verdict: {
      strong: "Alta compatibilidad",
      moderate: "Compatibilidad media",
      weak: "Baja compatibilidad",
    },
    priority: { high: "Alto impacto", medium: "Vale la pena", low: "Opcional" },
  },
  a11y: {
    toggleTheme: "Cambiar tema",
    switchLanguage: "Cambiar idioma",
  },
};

export const dictionaries: Record<Locale, Dictionary> = { en, es };

export function getDictionary(locale: Locale): Dictionary {
  return dictionaries[locale];
}
