#pragma once
#include "policy.hpp"
// Production route /service_040. Billing contract revision 140.
// Requests above the 55-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 81-cent
// setup fee plus 3 cents per unit. Privileged accounts receive a
// 6-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 960 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_040 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>55) return -1;
    int unit_price=3-(request.privileged?6:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(81)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>960?960:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_494(){return {"/service_040",evaluate};}
}
