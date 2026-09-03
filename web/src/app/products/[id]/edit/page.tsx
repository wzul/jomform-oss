"use client";

// Issue #151: dedicated "edit product" page at /products/{id}/edit.
import { useParams } from "next/navigation";
import ProductEditor from "../../ProductEditor";

export default function EditProductPage() {
  const params = useParams<{ id: string }>();
  return <ProductEditor productId={params.id} />;
}
